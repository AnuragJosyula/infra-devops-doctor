// Package aws provides a real infrastructure provider that discovers
// AWS resources (VPCs, EC2, RDS, S3, etc.) using the official AWS SDK.
package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/inframap/inframap/internal/graph"
	"github.com/inframap/inframap/internal/provider"
)

// ensure AWS implements Provider
var _ provider.Provider = (*AWS)(nil)

// AWS discovers infrastructure from a real AWS account.
type AWS struct {
	cfg aws.Config
}

// New creates a new AWS provider. It loads default credentials
// from ~/.aws/credentials, env vars, or IAM roles.
func New(ctx context.Context) (*AWS, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &AWS{cfg: cfg}, nil
}

func (a *AWS) Name() string        { return "aws" }
func (a *AWS) Description() string { return "Real AWS account (EC2, VPC, RDS, S3)" }

func (a *AWS) Watch(_ context.Context, _ chan<- graph.Event) error {
	return fmt.Errorf("aws watch not yet implemented")
}

func (a *AWS) Discover(ctx context.Context) (*graph.Graph, error) {
	g := graph.New()

	region := a.cfg.Region
	if region == "" {
		region = "unknown-region"
	}

	g.AddNode(&graph.Node{
		ID: fmt.Sprintf("region-%s", region), Type: "region", Provider: "aws",
		Name: region, Status: graph.StatusActive, Group: "AWS",
	})

	if err := a.discoverVPCsAndSubnets(ctx, g, region); err != nil {
		return nil, fmt.Errorf("vpcs: %w", err)
	}

	if err := a.discoverEC2(ctx, g, region); err != nil {
		return nil, fmt.Errorf("ec2: %w", err)
	}

	if err := a.discoverRDS(ctx, g, region); err != nil {
		// Log warning but don't fail discovery (might lack RDS permissions)
		fmt.Printf("⚠️ RDS discovery skipped: %v\n", err)
	}

	if err := a.discoverS3(ctx, g, region); err != nil {
		fmt.Printf("⚠️ S3 discovery skipped: %v\n", err)
	}

	return g, nil
}

func (a *AWS) discoverVPCsAndSubnets(ctx context.Context, g *graph.Graph, region string) error {
	client := ec2.NewFromConfig(a.cfg)
	
	// VPCs
	vpcOutput, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return err
	}

	for _, vpc := range vpcOutput.Vpcs {
		vpcID := aws.ToString(vpc.VpcId)
		name := getTag(vpc.Tags, "Name", vpcID)

		g.AddNode(&graph.Node{
			ID: vpcID, Type: "vpc", Provider: "aws",
			Name: name, Status: graph.StatusActive,
			Parent: fmt.Sprintf("region-%s", region), Group: "Networking",
			Metadata: map[string]string{
				"cidr": aws.ToString(vpc.CidrBlock),
				"is_default": fmt.Sprintf("%t", aws.ToBool(vpc.IsDefault)),
			},
		})
		g.AddEdge(&graph.Edge{
			ID: fmt.Sprintf("region-%s->%s", region, vpcID), Source: fmt.Sprintf("region-%s", region),
			Target: vpcID, Type: graph.EdgeContains,
		})
	}

	// Subnets
	subnetOutput, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{})
	if err != nil {
		return err
	}

	for _, sub := range subnetOutput.Subnets {
		subID := aws.ToString(sub.SubnetId)
		vpcID := aws.ToString(sub.VpcId)
		name := getTag(sub.Tags, "Name", subID)

		g.AddNode(&graph.Node{
			ID: subID, Type: "subnet", Provider: "aws",
			Name: name, Status: graph.StatusActive,
			Parent: vpcID, Group: "Networking",
			Metadata: map[string]string{
				"cidr": aws.ToString(sub.CidrBlock),
				"az": aws.ToString(sub.AvailabilityZone),
			},
		})
		g.AddEdge(&graph.Edge{
			ID: fmt.Sprintf("%s->%s", vpcID, subID), Source: vpcID,
			Target: subID, Type: graph.EdgeContains,
		})
	}

	return nil
}

func (a *AWS) discoverEC2(ctx context.Context, g *graph.Graph, region string) error {
	client := ec2.NewFromConfig(a.cfg)
	
	output, err := client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{})
	if err != nil {
		return err
	}

	for _, res := range output.Reservations {
		for _, inst := range res.Instances {
			instID := aws.ToString(inst.InstanceId)
			name := getTag(inst.Tags, "Name", instID)
			subID := aws.ToString(inst.SubnetId)
			
			status := graph.StatusUnknown
			if inst.State != nil {
				if inst.State.Name == types.InstanceStateNameTerminated || inst.State.Name == types.InstanceStateNameShuttingDown {
					continue // AWS keeps terminated instances in the API for a few hours; ignore them
				}
				if inst.State.Name == types.InstanceStateNameRunning {
					status = graph.StatusRunning
				} else if inst.State.Name == types.InstanceStateNameStopped {
					status = graph.StatusStopped
				}
			}

			meta := map[string]string{
				"instance_type": string(inst.InstanceType),
				"private_ip": aws.ToString(inst.PrivateIpAddress),
			}
			if inst.PublicIpAddress != nil {
				meta["public_ip"] = aws.ToString(inst.PublicIpAddress)
			}

			g.AddNode(&graph.Node{
				ID: instID, Type: "ec2", Provider: "aws",
				Name: name, Status: status,
				Parent: subID, Group: "Compute",
				Metadata: meta,
			})
			if subID != "" {
				g.AddEdge(&graph.Edge{
					ID: fmt.Sprintf("%s->%s", subID, instID), Source: subID,
					Target: instID, Type: graph.EdgeContains,
				})
			} else {
				g.AddEdge(&graph.Edge{
					ID: fmt.Sprintf("region-%s->%s", region, instID), Source: fmt.Sprintf("region-%s", region),
					Target: instID, Type: graph.EdgeContains,
				})
			}
		}
	}
	return nil
}

func (a *AWS) discoverRDS(ctx context.Context, g *graph.Graph, region string) error {
	client := rds.NewFromConfig(a.cfg)

	output, err := client.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{})
	if err != nil {
		return err
	}

	for _, db := range output.DBInstances {
		dbID := aws.ToString(db.DBInstanceIdentifier)
		subID := ""
		if db.DBSubnetGroup != nil && len(db.DBSubnetGroup.Subnets) > 0 {
			subID = aws.ToString(db.DBSubnetGroup.Subnets[0].SubnetIdentifier)
		}

		status := graph.StatusUnknown
		dbStatus := aws.ToString(db.DBInstanceStatus)
		if dbStatus == "deleting" || dbStatus == "deleted" {
			continue
		}
		if dbStatus == "available" {
			status = graph.StatusRunning
		} else if dbStatus == "stopped" {
			status = graph.StatusStopped
		}

		g.AddNode(&graph.Node{
			ID: dbID, Type: "rds", Provider: "aws",
			Name: dbID, Status: status,
			Parent: subID, Group: "Database",
			Metadata: map[string]string{
				"engine": aws.ToString(db.Engine),
				"version": aws.ToString(db.EngineVersion),
				"class": aws.ToString(db.DBInstanceClass),
			},
		})
		if subID != "" {
			g.AddEdge(&graph.Edge{
				ID: fmt.Sprintf("%s->%s", subID, dbID), Source: subID,
				Target: dbID, Type: graph.EdgeContains,
			})
		} else {
			g.AddEdge(&graph.Edge{
				ID: fmt.Sprintf("region-%s->%s", region, dbID), Source: fmt.Sprintf("region-%s", region),
				Target: dbID, Type: graph.EdgeContains,
			})
		}
	}
	return nil
}

func (a *AWS) discoverS3(ctx context.Context, g *graph.Graph, region string) error {
	client := s3.NewFromConfig(a.cfg)
	
	output, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return err
	}

	for _, b := range output.Buckets {
		bucketName := aws.ToString(b.Name)
		g.AddNode(&graph.Node{
			ID: bucketName, Type: "s3_bucket", Provider: "aws",
			Name: bucketName, Status: graph.StatusActive,
			Parent: fmt.Sprintf("region-%s", region), Group: "Storage",
		})
		g.AddEdge(&graph.Edge{
			ID: fmt.Sprintf("region-%s->%s", region, bucketName), Source: fmt.Sprintf("region-%s", region),
			Target: bucketName, Type: graph.EdgeContains,
		})
	}
	return nil
}

func getTag(tags []types.Tag, key, fallback string) string {
	for _, t := range tags {
		if aws.ToString(t.Key) == key {
			return aws.ToString(t.Value)
		}
	}
	return fallback
}
