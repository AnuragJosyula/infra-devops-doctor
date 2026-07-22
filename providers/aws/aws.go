// Package aws provides a real infrastructure provider that discovers
// AWS resources using the official AWS SDK.
//
// Discovery is paginated (large accounts are not silently truncated),
// runs across multiple regions concurrently, and derives real dependency
// edges — security-group rules become connects_to edges between the actual
// instances, load balancers route_to their targets — so blast-radius
// analysis has something meaningful to traverse.
package aws

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/inframap/inframap/internal/graph"
	"github.com/inframap/inframap/internal/provider"
)

// ensure AWS implements Provider
var _ provider.Provider = (*AWS)(nil)

// maxParallelRegions bounds concurrent region scans.
const maxParallelRegions = 6

// AWS discovers infrastructure from a real AWS account.
type AWS struct {
	cfg     aws.Config
	regions []string
}

// New creates a new AWS provider. It loads credentials from the default chain
// (~/.aws/credentials, SSO token cache, env vars, IAM roles), honouring
// AWS_PROFILE.
//
// regionSpec selects which regions to scan: "" uses the configured region,
// "all" enumerates every region enabled on the account, and a comma-separated
// list scans exactly those.
func New(ctx context.Context, regionSpec string) (*AWS, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	a := &AWS{cfg: cfg}
	a.regions, err = a.resolveRegions(ctx, regionSpec)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (a *AWS) resolveRegions(ctx context.Context, spec string) ([]string, error) {
	spec = strings.TrimSpace(spec)

	if strings.EqualFold(spec, "all") {
		client := ec2.NewFromConfig(a.cfg)
		out, err := client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to list regions (need ec2:DescribeRegions): %w", err)
		}
		var regions []string
		for _, r := range out.Regions {
			regions = append(regions, aws.ToString(r.RegionName))
		}
		sort.Strings(regions)
		return regions, nil
	}

	if spec != "" {
		var regions []string
		for _, r := range strings.Split(spec, ",") {
			if r = strings.TrimSpace(r); r != "" {
				regions = append(regions, r)
			}
		}
		if len(regions) > 0 {
			return regions, nil
		}
	}

	if a.cfg.Region == "" {
		return nil, fmt.Errorf("no AWS region configured (set AWS_REGION or run `aws configure`)")
	}
	return []string{a.cfg.Region}, nil
}

func (a *AWS) Name() string { return "aws" }

func (a *AWS) Description() string {
	return fmt.Sprintf("Real AWS account (%s)", strings.Join(a.regions, ", "))
}

func (a *AWS) Watch(_ context.Context, _ chan<- graph.Event) error {
	return fmt.Errorf("aws watch not yet implemented")
}

// Discover scans global services once and every selected region concurrently.
func (a *AWS) Discover(ctx context.Context) (*graph.Graph, error) {
	g := graph.New()

	// ─── Global services ────────────────────────────────
	roleTargets := a.discoverIAM(ctx, g)
	a.discoverS3(ctx, g)

	// ─── Regional services ──────────────────────────────
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxParallelRegions)
	var firstErr error
	var errMu sync.Mutex

	for _, region := range a.regions {
		wg.Add(1)
		go func(region string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if err := a.discoverRegion(ctx, g, region, roleTargets); err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			}
		}(region)
	}
	wg.Wait()

	if len(g.Nodes) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return g, nil
}

func (a *AWS) discoverRegion(ctx context.Context, g *graph.Graph, region string, roleTargets map[string][]string) error {
	cfg := a.cfg.Copy()
	cfg.Region = region

	regionID := regionNodeID(region)
	g.AddNode(&graph.Node{
		ID: regionID, Type: "region", Provider: "aws",
		Name: region, Status: graph.StatusActive, Region: region, Group: "AWS",
	})

	if err := a.discoverNetwork(ctx, cfg, g, region); err != nil {
		return fmt.Errorf("%s: network discovery failed: %w", region, err)
	}

	// Security groups must be discovered before instances/databases so their
	// attachments and rule-derived edges can be wired up.
	sgRules := a.discoverSecurityGroups(ctx, cfg, g, region)
	sgMembers := map[string][]string{} // security group ID -> attached resource IDs
	var membersMu sync.Mutex

	a.discoverEC2(ctx, cfg, g, region, sgMembers, &membersMu, roleTargets)
	a.discoverVolumes(ctx, cfg, g, region)
	a.discoverRDS(ctx, cfg, g, region, sgMembers, &membersMu)
	a.discoverLoadBalancers(ctx, cfg, g, region, sgMembers, &membersMu)
	a.discoverLambda(ctx, cfg, g, region)
	a.discoverECS(ctx, cfg, g, region)

	// Turn "SG-A allows inbound from SG-B" into connects_to edges between the
	// resources actually in those groups. This is what gives blast radius
	// something real to traverse.
	a.linkSecurityGroupRules(g, sgRules, sgMembers)
	return nil
}

// ─── Networking ─────────────────────────────────────────

func (a *AWS) discoverNetwork(ctx context.Context, cfg aws.Config, g *graph.Graph, region string) error {
	client := ec2.NewFromConfig(cfg)
	regionID := regionNodeID(region)

	vpcPager := ec2.NewDescribeVpcsPaginator(client, &ec2.DescribeVpcsInput{})
	for vpcPager.HasMorePages() {
		page, err := vpcPager.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, vpc := range page.Vpcs {
			id := aws.ToString(vpc.VpcId)
			g.AddNode(&graph.Node{
				ID: id, Type: "vpc", Provider: "aws",
				Name: getTag(vpc.Tags, "Name", id), Status: graph.StatusActive,
				Region: region, Parent: regionID, Group: "Networking",
				Metadata: map[string]string{
					"cidr":       aws.ToString(vpc.CidrBlock),
					"is_default": fmt.Sprintf("%t", aws.ToBool(vpc.IsDefault)),
				},
			})
			addEdge(g, regionID, id, graph.EdgeContains)
		}
	}

	subnetPager := ec2.NewDescribeSubnetsPaginator(client, &ec2.DescribeSubnetsInput{})
	for subnetPager.HasMorePages() {
		page, err := subnetPager.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, sub := range page.Subnets {
			id := aws.ToString(sub.SubnetId)
			vpcID := aws.ToString(sub.VpcId)
			g.AddNode(&graph.Node{
				ID: id, Type: "subnet", Provider: "aws",
				Name: getTag(sub.Tags, "Name", id), Status: graph.StatusActive,
				Region: region, Parent: vpcID, Group: "Networking",
				Metadata: map[string]string{
					"cidr":        aws.ToString(sub.CidrBlock),
					"az":          aws.ToString(sub.AvailabilityZone),
					"public":      fmt.Sprintf("%t", aws.ToBool(sub.MapPublicIpOnLaunch)),
					"free_ip_count": fmt.Sprintf("%d", aws.ToInt32(sub.AvailableIpAddressCount)),
				},
			})
			addEdge(g, vpcID, id, graph.EdgeContains)
		}
	}

	igwPager := ec2.NewDescribeInternetGatewaysPaginator(client, &ec2.DescribeInternetGatewaysInput{})
	for igwPager.HasMorePages() {
		page, err := igwPager.NextPage(ctx)
		if err != nil {
			break // non-fatal
		}
		for _, igw := range page.InternetGateways {
			id := aws.ToString(igw.InternetGatewayId)
			parent := regionID
			if len(igw.Attachments) > 0 {
				parent = aws.ToString(igw.Attachments[0].VpcId)
			}
			g.AddNode(&graph.Node{
				ID: id, Type: "internet_gateway", Provider: "aws",
				Name: getTag(igw.Tags, "Name", id), Status: graph.StatusActive,
				Region: region, Parent: parent, Group: "Networking",
			})
			addEdge(g, parent, id, graph.EdgeContains)
		}
	}

	natPager := ec2.NewDescribeNatGatewaysPaginator(client, &ec2.DescribeNatGatewaysInput{})
	for natPager.HasMorePages() {
		page, err := natPager.NextPage(ctx)
		if err != nil {
			break // non-fatal
		}
		for _, nat := range page.NatGateways {
			if nat.State == ec2types.NatGatewayStateDeleted || nat.State == ec2types.NatGatewayStateDeleting {
				continue
			}
			id := aws.ToString(nat.NatGatewayId)
			subnetID := aws.ToString(nat.SubnetId)
			meta := map[string]string{"state": string(nat.State)}
			if len(nat.NatGatewayAddresses) > 0 {
				meta["elastic_ip"] = aws.ToString(nat.NatGatewayAddresses[0].PublicIp)
			}
			g.AddNode(&graph.Node{
				ID: id, Type: "nat_gateway", Provider: "aws",
				Name: getTag(nat.Tags, "Name", id), Status: graph.StatusActive,
				Region: region, Parent: subnetID, Group: "Networking", Metadata: meta,
			})
			addEdge(g, subnetID, id, graph.EdgeContains)
		}
	}

	return nil
}

// ─── Security groups ────────────────────────────────────

// sgRule is one inbound rule that references another security group.
type sgRule struct {
	targetSG string // the group the rule belongs to (traffic destination)
	sourceSG string // the group allowed to connect (traffic origin)
	port     string
}

func (a *AWS) discoverSecurityGroups(ctx context.Context, cfg aws.Config, g *graph.Graph, region string) []sgRule {
	client := ec2.NewFromConfig(cfg)
	var rules []sgRule

	pager := ec2.NewDescribeSecurityGroupsPaginator(client, &ec2.DescribeSecurityGroupsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			log.Printf("⚠️  %s: security group discovery skipped: %v", region, err)
			return rules
		}
		for _, sg := range page.SecurityGroups {
			id := aws.ToString(sg.GroupId)
			vpcID := aws.ToString(sg.VpcId)
			parent := vpcID
			if parent == "" {
				parent = regionNodeID(region)
			}

			g.AddNode(&graph.Node{
				ID: id, Type: "security_group", Provider: "aws",
				Name: aws.ToString(sg.GroupName), Status: graph.StatusActive,
				Region: region, Parent: parent, Group: "Security",
				Metadata: map[string]string{
					"inbound":     formatIngress(sg.IpPermissions),
					"outbound":    formatIngress(sg.IpPermissionsEgress),
					"description": aws.ToString(sg.Description),
					"vpc":         vpcID,
				},
			})
			addEdge(g, parent, id, graph.EdgeContains)

			for _, perm := range sg.IpPermissions {
				for _, pair := range perm.UserIdGroupPairs {
					if src := aws.ToString(pair.GroupId); src != "" {
						rules = append(rules, sgRule{targetSG: id, sourceSG: src, port: portRange(perm)})
					}
				}
			}
		}
	}
	return rules
}

// linkSecurityGroupRules converts group-to-group rules into resource-level
// connects_to edges: every member of the source group can reach every member
// of the target group.
func (a *AWS) linkSecurityGroupRules(g *graph.Graph, rules []sgRule, members map[string][]string) {
	for _, r := range rules {
		for _, src := range members[r.sourceSG] {
			for _, dst := range members[r.targetSG] {
				if src == dst {
					continue
				}
				label := "allows " + r.port
				g.AddEdge(&graph.Edge{
					ID:     fmt.Sprintf("%s->%s:%s", src, dst, r.port),
					Source: src, Target: dst, Type: graph.EdgeConnectsTo,
					Label: label,
				})
			}
		}
	}
}

// ─── Compute ────────────────────────────────────────────

func (a *AWS) discoverEC2(ctx context.Context, cfg aws.Config, g *graph.Graph, region string,
	sgMembers map[string][]string, mu *sync.Mutex, roleTargets map[string][]string) {

	client := ec2.NewFromConfig(cfg)
	regionID := regionNodeID(region)
	// instance profile ARN -> instance IDs, so IAM roles can be attached
	profileToInstances := map[string][]string{}

	pager := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			log.Printf("⚠️  %s: EC2 discovery skipped: %v", region, err)
			return
		}
		for _, res := range page.Reservations {
			for _, inst := range res.Instances {
				id := aws.ToString(inst.InstanceId)

				status := graph.StatusUnknown
				if inst.State != nil {
					switch inst.State.Name {
					case ec2types.InstanceStateNameTerminated, ec2types.InstanceStateNameShuttingDown:
						continue // AWS keeps terminated instances around for ~1h
					case ec2types.InstanceStateNameRunning:
						status = graph.StatusRunning
					case ec2types.InstanceStateNameStopped:
						status = graph.StatusStopped
					}
				}

				meta := map[string]string{
					"instance_type": string(inst.InstanceType),
					"private_ip":    aws.ToString(inst.PrivateIpAddress),
					"ami":           aws.ToString(inst.ImageId),
					"az":            azOf(inst.Placement),
				}
				if ip := aws.ToString(inst.PublicIpAddress); ip != "" {
					meta["public_ip"] = ip
				}

				parent := aws.ToString(inst.SubnetId)
				if parent == "" {
					parent = regionID
				}

				g.AddNode(&graph.Node{
					ID: id, Type: "ec2", Provider: "aws",
					Name: getTag(inst.Tags, "Name", id), Status: status,
					Region: region, Parent: parent, Group: "Compute", Metadata: meta,
				})
				addEdge(g, parent, id, graph.EdgeContains)

				for _, sg := range inst.SecurityGroups {
					sgID := aws.ToString(sg.GroupId)
					addEdge(g, sgID, id, graph.EdgeAttachedTo)
					mu.Lock()
					sgMembers[sgID] = append(sgMembers[sgID], id)
					mu.Unlock()
				}

				if inst.IamInstanceProfile != nil {
					arn := aws.ToString(inst.IamInstanceProfile.Arn)
					profileToInstances[arn] = append(profileToInstances[arn], id)
				}
			}
		}
	}

	// Attach IAM roles by matching the role name inside the instance profile ARN.
	for arn, instances := range profileToInstances {
		name := arnTail(arn)
		for roleID, _ := range roleTargets {
			if strings.EqualFold(arnTail(roleID), name) || strings.EqualFold(roleID, name) {
				for _, inst := range instances {
					addEdge(g, roleID, inst, graph.EdgeAttachedTo)
				}
			}
		}
	}
}

func (a *AWS) discoverVolumes(ctx context.Context, cfg aws.Config, g *graph.Graph, region string) {
	client := ec2.NewFromConfig(cfg)
	pager := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return // non-fatal
		}
		for _, v := range page.Volumes {
			id := aws.ToString(v.VolumeId)
			parent := regionNodeID(region)
			if len(v.Attachments) > 0 {
				if inst := aws.ToString(v.Attachments[0].InstanceId); inst != "" {
					parent = inst
				}
			}
			g.AddNode(&graph.Node{
				ID: id, Type: "volume", Provider: "aws",
				Name: getTag(v.Tags, "Name", id), Status: graph.StatusActive,
				Region: region, Parent: parent, Group: "Storage",
				Metadata: map[string]string{
					"size":      fmt.Sprintf("%d GiB", aws.ToInt32(v.Size)),
					"encrypted": fmt.Sprintf("%t", aws.ToBool(v.Encrypted)),
					"type":      string(v.VolumeType),
				},
			})
			addEdge(g, parent, id, graph.EdgeAttachedTo)
		}
	}
}

func (a *AWS) discoverRDS(ctx context.Context, cfg aws.Config, g *graph.Graph, region string,
	sgMembers map[string][]string, mu *sync.Mutex) {

	client := rds.NewFromConfig(cfg)
	pager := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			log.Printf("⚠️  %s: RDS discovery skipped: %v", region, err)
			return
		}
		for _, db := range page.DBInstances {
			id := aws.ToString(db.DBInstanceIdentifier)

			state := strings.ToLower(aws.ToString(db.DBInstanceStatus))
			if state == "deleting" || state == "deleted" {
				continue
			}
			status := graph.StatusUnknown
			switch state {
			case "available":
				status = graph.StatusRunning
			case "stopped":
				status = graph.StatusStopped
			}

			parent := regionNodeID(region)
			if db.DBSubnetGroup != nil && len(db.DBSubnetGroup.Subnets) > 0 {
				parent = aws.ToString(db.DBSubnetGroup.Subnets[0].SubnetIdentifier)
			}

			g.AddNode(&graph.Node{
				ID: id, Type: "rds", Provider: "aws",
				Name: id, Status: status,
				Region: region, Parent: parent, Group: "Database",
				Metadata: map[string]string{
					"engine":              aws.ToString(db.Engine),
					"version":             aws.ToString(db.EngineVersion),
					"instance_class":      aws.ToString(db.DBInstanceClass),
					"multi_az":            fmt.Sprintf("%t", aws.ToBool(db.MultiAZ)),
					"publicly_accessible": fmt.Sprintf("%t", aws.ToBool(db.PubliclyAccessible)),
					"encrypted":           fmt.Sprintf("%t", aws.ToBool(db.StorageEncrypted)),
					"storage":             fmt.Sprintf("%d GiB", aws.ToInt32(db.AllocatedStorage)),
					"endpoint":            endpointOf(db.Endpoint),
				},
			})
			addEdge(g, parent, id, graph.EdgeContains)

			for _, sg := range db.VpcSecurityGroups {
				sgID := aws.ToString(sg.VpcSecurityGroupId)
				addEdge(g, sgID, id, graph.EdgeAttachedTo)
				mu.Lock()
				sgMembers[sgID] = append(sgMembers[sgID], id)
				mu.Unlock()
			}
		}
	}
}

func (a *AWS) discoverLoadBalancers(ctx context.Context, cfg aws.Config, g *graph.Graph, region string,
	sgMembers map[string][]string, mu *sync.Mutex) {

	client := elasticloadbalancingv2.NewFromConfig(cfg)

	var lbs []elbtypes.LoadBalancer
	pager := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(client, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			log.Printf("⚠️  %s: load balancer discovery skipped: %v", region, err)
			return
		}
		lbs = append(lbs, page.LoadBalancers...)
	}

	for _, lb := range lbs {
		arn := aws.ToString(lb.LoadBalancerArn)
		name := aws.ToString(lb.LoadBalancerName)

		parent := regionNodeID(region)
		if len(lb.AvailabilityZones) > 0 {
			if sub := aws.ToString(lb.AvailabilityZones[0].SubnetId); sub != "" {
				parent = sub
			}
		}

		typ := "alb"
		if lb.Type == elbtypes.LoadBalancerTypeEnumNetwork {
			typ = "load_balancer"
		}

		meta := map[string]string{
			"scheme": string(lb.Scheme),
			"type":   string(lb.Type),
			"dns":    aws.ToString(lb.DNSName),
		}
		if lo, err := client.DescribeListeners(ctx, &elasticloadbalancingv2.DescribeListenersInput{
			LoadBalancerArn: lb.LoadBalancerArn,
		}); err == nil {
			var parts []string
			for _, l := range lo.Listeners {
				parts = append(parts, fmt.Sprintf("%s:%d", l.Protocol, aws.ToInt32(l.Port)))
			}
			meta["listeners"] = strings.Join(parts, ", ")
		}

		g.AddNode(&graph.Node{
			ID: arn, Type: typ, Provider: "aws",
			Name: name, Status: graph.StatusActive,
			Region: region, Parent: parent, Group: "Compute", Metadata: meta,
		})
		addEdge(g, parent, arn, graph.EdgeContains)

		for _, sgID := range lb.SecurityGroups {
			addEdge(g, sgID, arn, graph.EdgeAttachedTo)
			mu.Lock()
			sgMembers[sgID] = append(sgMembers[sgID], arn)
			mu.Unlock()
		}

		// target groups -> routes_to edges to the registered instances
		tgOut, err := client.DescribeTargetGroups(ctx, &elasticloadbalancingv2.DescribeTargetGroupsInput{
			LoadBalancerArn: lb.LoadBalancerArn,
		})
		if err != nil {
			continue
		}
		for _, tg := range tgOut.TargetGroups {
			health, err := client.DescribeTargetHealth(ctx, &elasticloadbalancingv2.DescribeTargetHealthInput{
				TargetGroupArn: tg.TargetGroupArn,
			})
			if err != nil {
				continue
			}
			for _, t := range health.TargetHealthDescriptions {
				if t.Target == nil {
					continue
				}
				if targetID := aws.ToString(t.Target.Id); targetID != "" {
					g.AddEdge(&graph.Edge{
						ID:     fmt.Sprintf("%s->%s", arn, targetID),
						Source: arn, Target: targetID, Type: graph.EdgeRoutesTo,
						Label: aws.ToString(tg.TargetGroupName),
					})
				}
			}
		}
	}
}

func (a *AWS) discoverLambda(ctx context.Context, cfg aws.Config, g *graph.Graph, region string) {
	client := lambda.NewFromConfig(cfg)
	regionID := regionNodeID(region)

	pager := lambda.NewListFunctionsPaginator(client, &lambda.ListFunctionsInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			log.Printf("⚠️  %s: Lambda discovery skipped: %v", region, err)
			return
		}
		for _, fn := range page.Functions {
			name := aws.ToString(fn.FunctionName)
			id := aws.ToString(fn.FunctionArn)

			meta := map[string]string{
				"runtime": string(fn.Runtime),
				"memory":  fmt.Sprintf("%d", aws.ToInt32(fn.MemorySize)),
				"timeout": fmt.Sprintf("%d", aws.ToInt32(fn.Timeout)),
				"handler": aws.ToString(fn.Handler),
			}

			parent := regionID
			if fn.VpcConfig != nil && len(fn.VpcConfig.SubnetIds) > 0 {
				parent = fn.VpcConfig.SubnetIds[0]
				for _, sgID := range fn.VpcConfig.SecurityGroupIds {
					addEdge(g, sgID, id, graph.EdgeAttachedTo)
				}
			}

			g.AddNode(&graph.Node{
				ID: id, Type: "lambda", Provider: "aws",
				Name: name, Status: graph.StatusActive,
				Region: region, Parent: parent, Group: "Compute", Metadata: meta,
			})
			addEdge(g, parent, id, graph.EdgeContains)
		}
	}
}

func (a *AWS) discoverECS(ctx context.Context, cfg aws.Config, g *graph.Graph, region string) {
	client := ecs.NewFromConfig(cfg)
	regionID := regionNodeID(region)

	clusterPager := ecs.NewListClustersPaginator(client, &ecs.ListClustersInput{})
	for clusterPager.HasMorePages() {
		page, err := clusterPager.NextPage(ctx)
		if err != nil {
			log.Printf("⚠️  %s: ECS discovery skipped: %v", region, err)
			return
		}
		for _, clusterArn := range page.ClusterArns {
			clusterName := arnTail(clusterArn)
			g.AddNode(&graph.Node{
				ID: clusterArn, Type: "ecs_cluster", Provider: "aws",
				Name: clusterName, Status: graph.StatusActive,
				Region: region, Parent: regionID, Group: "Compute",
			})
			addEdge(g, regionID, clusterArn, graph.EdgeContains)

			svcPager := ecs.NewListServicesPaginator(client, &ecs.ListServicesInput{Cluster: aws.String(clusterArn)})
			for svcPager.HasMorePages() {
				svcPage, err := svcPager.NextPage(ctx)
				if err != nil {
					break
				}
				if len(svcPage.ServiceArns) == 0 {
					continue
				}
				desc, err := client.DescribeServices(ctx, &ecs.DescribeServicesInput{
					Cluster: aws.String(clusterArn), Services: svcPage.ServiceArns,
				})
				if err != nil {
					continue
				}
				for _, svc := range desc.Services {
					svcArn := aws.ToString(svc.ServiceArn)
					status := graph.StatusRunning
					if !strings.EqualFold(aws.ToString(svc.Status), "ACTIVE") {
						status = graph.StatusUnknown
					}
					g.AddNode(&graph.Node{
						ID: svcArn, Type: "ecs_service", Provider: "aws",
						Name: aws.ToString(svc.ServiceName), Status: status,
						Region: region, Parent: clusterArn, Group: "Compute",
						Metadata: map[string]string{
							"launch_type":   string(svc.LaunchType),
							"desired_count": fmt.Sprintf("%d", svc.DesiredCount),
							"running_count": fmt.Sprintf("%d", svc.RunningCount),
						},
					})
					addEdge(g, clusterArn, svcArn, graph.EdgeContains)

					// services fronted by a load balancer route from it
					for _, lb := range svc.LoadBalancers {
						if tg := aws.ToString(lb.TargetGroupArn); tg != "" {
							g.AddEdge(&graph.Edge{
								ID:     fmt.Sprintf("%s->%s", tg, svcArn),
								Source: tg, Target: svcArn, Type: graph.EdgeRoutesTo,
							})
						}
					}
				}
			}
		}
	}
}

// ─── Global services ────────────────────────────────────

// discoverIAM returns a set of role node IDs keyed by role name.
func (a *AWS) discoverIAM(ctx context.Context, g *graph.Graph) map[string][]string {
	roles := map[string][]string{}
	client := iam.NewFromConfig(a.cfg)

	pager := iam.NewListRolesPaginator(client, &iam.ListRolesInput{})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			log.Printf("⚠️  IAM discovery skipped: %v", err)
			return roles
		}
		for _, r := range page.Roles {
			name := aws.ToString(r.RoleName)
			// service-linked roles are noise
			if strings.HasPrefix(aws.ToString(r.Path), "/aws-service-role/") {
				continue
			}
			id := "iam-role/" + name

			var policies []string
			if att, err := client.ListAttachedRolePolicies(ctx, &iam.ListAttachedRolePoliciesInput{
				RoleName: r.RoleName,
			}); err == nil {
				for _, p := range att.AttachedPolicies {
					policies = append(policies, aws.ToString(p.PolicyName))
				}
			}

			g.AddNode(&graph.Node{
				ID: id, Type: "iam_role", Provider: "aws",
				Name: name, Status: graph.StatusActive, Group: "Security",
				Metadata: map[string]string{
					"policies": strings.Join(policies, ", "),
					"arn":      aws.ToString(r.Arn),
				},
			})
			roles[id] = nil
		}
	}
	return roles
}

func (a *AWS) discoverS3(ctx context.Context, g *graph.Graph) {
	client := s3.NewFromConfig(a.cfg)
	out, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		log.Printf("⚠️  S3 discovery skipped: %v", err)
		return
	}

	// Bucket settings need one call each; bound the concurrency.
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)

	for _, b := range out.Buckets {
		name := aws.ToString(b.Name)
		if name == "" {
			continue
		}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			region := a.cfg.Region
			if loc, err := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: aws.String(name)}); err == nil {
				if r := string(loc.LocationConstraint); r != "" {
					region = r
				} else {
					region = "us-east-1" // empty constraint means us-east-1
				}
			}

			meta := map[string]string{
				"encryption": bucketEncryption(ctx, client, name),
				"versioning": bucketVersioning(ctx, client, name),
				"public":     bucketPublic(ctx, client, name),
			}

			parent := regionNodeID(region)
			g.AddNode(&graph.Node{
				ID: name, Type: "s3_bucket", Provider: "aws",
				Name: name, Status: graph.StatusActive,
				Region: region, Parent: parent, Group: "Storage", Metadata: meta,
			})
			addEdge(g, parent, name, graph.EdgeContains)
		}(name)
	}
	wg.Wait()
}

func bucketEncryption(ctx context.Context, client *s3.Client, bucket string) string {
	out, err := client.GetBucketEncryption(ctx, &s3.GetBucketEncryptionInput{Bucket: aws.String(bucket)})
	if err != nil || out.ServerSideEncryptionConfiguration == nil || len(out.ServerSideEncryptionConfiguration.Rules) == 0 {
		return "none"
	}
	rule := out.ServerSideEncryptionConfiguration.Rules[0]
	if rule.ApplyServerSideEncryptionByDefault == nil {
		return "none"
	}
	return string(rule.ApplyServerSideEncryptionByDefault.SSEAlgorithm)
}

func bucketVersioning(ctx context.Context, client *s3.Client, bucket string) string {
	out, err := client.GetBucketVersioning(ctx, &s3.GetBucketVersioningInput{Bucket: aws.String(bucket)})
	if err != nil || string(out.Status) == "" {
		return "disabled"
	}
	if strings.EqualFold(string(out.Status), "Enabled") {
		return "enabled"
	}
	return "disabled"
}

// bucketPublic reports "true" when public access is not fully blocked.
func bucketPublic(ctx context.Context, client *s3.Client, bucket string) string {
	out, err := client.GetPublicAccessBlock(ctx, &s3.GetPublicAccessBlockInput{Bucket: aws.String(bucket)})
	if err != nil || out.PublicAccessBlockConfiguration == nil {
		return "true" // no block configuration at all
	}
	c := out.PublicAccessBlockConfiguration
	fullyBlocked := aws.ToBool(c.BlockPublicAcls) && aws.ToBool(c.BlockPublicPolicy) &&
		aws.ToBool(c.IgnorePublicAcls) && aws.ToBool(c.RestrictPublicBuckets)
	return fmt.Sprintf("%t", !fullyBlocked)
}

// ─── Helpers ────────────────────────────────────────────

func regionNodeID(region string) string { return "region-" + region }

func addEdge(g *graph.Graph, source, target, typ string) {
	if source == "" || target == "" || source == target {
		return
	}
	g.AddEdge(&graph.Edge{
		ID: fmt.Sprintf("%s->%s", source, target), Source: source, Target: target, Type: typ,
	})
}

func getTag(tags []ec2types.Tag, key, fallback string) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == key {
			if v := aws.ToString(t.Value); v != "" {
				return v
			}
		}
	}
	return fallback
}

// formatIngress renders permissions the way the doctor rules expect,
// e.g. "22 from 0.0.0.0/0, 443 from 0.0.0.0/0".
func formatIngress(perms []ec2types.IpPermission) string {
	var parts []string
	for _, p := range perms {
		port := portRange(p)
		for _, r := range p.IpRanges {
			parts = append(parts, fmt.Sprintf("%s from %s", port, aws.ToString(r.CidrIp)))
		}
		for _, r := range p.Ipv6Ranges {
			parts = append(parts, fmt.Sprintf("%s from %s", port, aws.ToString(r.CidrIpv6)))
		}
		for _, gp := range p.UserIdGroupPairs {
			parts = append(parts, fmt.Sprintf("%s from %s", port, aws.ToString(gp.GroupId)))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

func portRange(p ec2types.IpPermission) string {
	if aws.ToString(p.IpProtocol) == "-1" {
		return "all"
	}
	from, to := aws.ToInt32(p.FromPort), aws.ToInt32(p.ToPort)
	if from == to {
		return fmt.Sprintf("%d", from)
	}
	return fmt.Sprintf("%d-%d", from, to)
}

func azOf(p *ec2types.Placement) string {
	if p == nil {
		return ""
	}
	return aws.ToString(p.AvailabilityZone)
}

func endpointOf(e *rdstypes.Endpoint) string {
	if e == nil {
		return ""
	}
	return aws.ToString(e.Address)
}

func arnTail(arn string) string {
	if i := strings.LastIndex(arn, "/"); i >= 0 {
		return arn[i+1:]
	}
	if i := strings.LastIndex(arn, ":"); i >= 0 {
		return arn[i+1:]
	}
	return arn
}
