// Package mock provides a simulated AWS infrastructure for development and demos.
// It generates ~60 interconnected nodes with realistic relationships.
package mock

import (
	"context"
	"fmt"

	"github.com/inframap/inframap/internal/graph"
	"github.com/inframap/inframap/internal/provider"
)

// ensure Mock implements Provider
var _ provider.Provider = (*Mock)(nil)

// Mock generates a realistic simulated infrastructure graph.
type Mock struct{}

// New creates a new mock provider.
func New() *Mock {
	return &Mock{}
}

func (m *Mock) Name() string        { return "mock" }
func (m *Mock) Description() string { return "Simulated AWS infrastructure for demos and development" }

// Watch is not supported for mock.
func (m *Mock) Watch(_ context.Context, _ chan<- graph.Event) error {
	return fmt.Errorf("mock provider does not support live watching")
}

// Discover generates a complete simulated infrastructure.
func (m *Mock) Discover(_ context.Context) (*graph.Graph, error) {
	g := graph.New()

	// ─── Region ─────────────────────────────────────────
	addNode(g, "region-us-east-1", "region", "us-east-1", graph.StatusActive, "", "AWS Region", nil)

	// ─── VPC ────────────────────────────────────────────
	addNode(g, "vpc-main", "vpc", "main-vpc", graph.StatusActive, "region-us-east-1", "Networking", map[string]string{
		"cidr": "10.0.0.0/16", "dns_support": "true", "dns_hostnames": "true",
	})
	addEdge(g, "region-us-east-1", "vpc-main", graph.EdgeContains)

	// ─── Internet Gateway ──────────────────────────────
	addNode(g, "igw-main", "internet_gateway", "main-igw", graph.StatusActive, "vpc-main", "Networking", nil)
	addEdge(g, "vpc-main", "igw-main", graph.EdgeContains)

	// ─── NAT Gateway ───────────────────────────────────
	addNode(g, "nat-main", "nat_gateway", "main-nat", graph.StatusActive, "vpc-main", "Networking", map[string]string{
		"elastic_ip": "54.123.45.67", "subnet": "subnet-public-1a",
	})
	addEdge(g, "vpc-main", "nat-main", graph.EdgeContains)

	// ─── Subnets ────────────────────────────────────────
	subnets := []struct {
		id, name, az, cidr, tier string
	}{
		{"subnet-public-1a", "public-1a", "us-east-1a", "10.0.1.0/24", "public"},
		{"subnet-public-1b", "public-1b", "us-east-1b", "10.0.2.0/24", "public"},
		{"subnet-private-1a", "private-1a", "us-east-1a", "10.0.10.0/24", "private"},
		{"subnet-private-1b", "private-1b", "us-east-1b", "10.0.11.0/24", "private"},
		{"subnet-data-1a", "data-1a", "us-east-1a", "10.0.20.0/24", "data"},
		{"subnet-data-1b", "data-1b", "us-east-1b", "10.0.21.0/24", "data"},
	}
	for _, s := range subnets {
		addNode(g, s.id, "subnet", s.name, graph.StatusActive, "vpc-main", "Networking", map[string]string{
			"cidr": s.cidr, "az": s.az, "tier": s.tier,
		})
		addEdge(g, "vpc-main", s.id, graph.EdgeContains)
	}

	// ─── Security Groups ────────────────────────────────
	sgs := []struct {
		id, name, desc string
		rules          map[string]string
	}{
		{"sg-alb", "alb-sg", "ALB Security Group", map[string]string{
			"inbound": "80,443 from 0.0.0.0/0", "outbound": "all",
		}},
		{"sg-web", "web-sg", "Web Server Security Group", map[string]string{
			"inbound": "8080 from sg-alb", "outbound": "all",
		}},
		{"sg-db", "db-sg", "Database Security Group", map[string]string{
			"inbound": "5432 from sg-web", "outbound": "all",
		}},
		{"sg-cache", "cache-sg", "Cache Security Group", map[string]string{
			"inbound": "6379 from sg-web", "outbound": "all",
		}},
	}
	for _, sg := range sgs {
		addNode(g, sg.id, "security_group", sg.name, graph.StatusActive, "vpc-main", "Security", sg.rules)
		addEdge(g, "vpc-main", sg.id, graph.EdgeContains)
	}

	// ─── ALB ────────────────────────────────────────────
	addNode(g, "alb-web", "alb", "web-alb", graph.StatusActive, "subnet-public-1a", "Compute", map[string]string{
		"scheme": "internet-facing", "type": "application",
		"dns": "web-alb-123456.us-east-1.elb.amazonaws.com",
		"listeners": "HTTPS:443, HTTP:80",
	})
	addEdge(g, "subnet-public-1a", "alb-web", graph.EdgeContains)
	addEdge(g, "sg-alb", "alb-web", graph.EdgeAttachedTo)

	// ─── EC2 Instances (ASG) ────────────────────────────
	ec2Instances := []struct {
		id, name, subnet, az string
	}{
		{"ec2-web-1", "web-server-1", "subnet-private-1a", "us-east-1a"},
		{"ec2-web-2", "web-server-2", "subnet-private-1a", "us-east-1a"},
		{"ec2-web-3", "web-server-3", "subnet-private-1b", "us-east-1b"},
		{"ec2-web-4", "web-server-4", "subnet-private-1b", "us-east-1b"},
	}
	for _, ec2 := range ec2Instances {
		addNode(g, ec2.id, "ec2", ec2.name, graph.StatusRunning, ec2.subnet, "Compute", map[string]string{
			"instance_type": "t3.medium", "ami": "ami-0abcdef1234567890",
			"az": ec2.az, "private_ip": fmt.Sprintf("10.0.%s", ec2.id),
		})
		addEdge(g, ec2.subnet, ec2.id, graph.EdgeContains)
		addEdge(g, "alb-web", ec2.id, graph.EdgeRoutesTo)
		addEdge(g, "sg-web", ec2.id, graph.EdgeAttachedTo)
	}

	// ─── RDS ────────────────────────────────────────────
	addNode(g, "rds-primary", "rds", "main-postgres", graph.StatusRunning, "subnet-data-1a", "Database", map[string]string{
		"engine": "postgres", "version": "15.4", "instance_class": "db.r6g.large",
		"multi_az": "true", "storage": "100 GiB gp3", "endpoint": "main-postgres.abc123.us-east-1.rds.amazonaws.com",
	})
	addEdge(g, "subnet-data-1a", "rds-primary", graph.EdgeContains)
	addEdge(g, "sg-db", "rds-primary", graph.EdgeAttachedTo)

	addNode(g, "rds-replica", "rds", "main-postgres-replica", graph.StatusRunning, "subnet-data-1b", "Database", map[string]string{
		"engine": "postgres", "version": "15.4", "instance_class": "db.r6g.large",
		"role": "read-replica", "replication_source": "rds-primary",
	})
	addEdge(g, "subnet-data-1b", "rds-replica", graph.EdgeContains)
	addEdge(g, "sg-db", "rds-replica", graph.EdgeAttachedTo)
	addEdge(g, "rds-primary", "rds-replica", graph.EdgeConnectsTo)

	// EC2 → RDS connections
	for _, ec2 := range ec2Instances {
		addEdge(g, ec2.id, "rds-primary", graph.EdgeConnectsTo)
	}

	// ─── ElastiCache ────────────────────────────────────
	addNode(g, "cache-session", "elasticache", "session-cache", graph.StatusRunning, "subnet-data-1a", "Database", map[string]string{
		"engine": "redis", "version": "7.0", "node_type": "cache.r6g.large",
		"num_nodes": "2", "endpoint": "session-cache.abc123.cache.amazonaws.com",
	})
	addEdge(g, "subnet-data-1a", "cache-session", graph.EdgeContains)
	addEdge(g, "sg-cache", "cache-session", graph.EdgeAttachedTo)

	// EC2 → ElastiCache connections
	for _, ec2 := range ec2Instances {
		addEdge(g, ec2.id, "cache-session", graph.EdgeConnectsTo)
	}

	// ─── S3 Buckets ─────────────────────────────────────
	addNode(g, "s3-assets", "s3_bucket", "assets-cdn-bucket", graph.StatusActive, "region-us-east-1", "Storage", map[string]string{
		"versioning": "enabled", "encryption": "AES256", "public": "false",
		"lifecycle": "transition to IA after 90 days",
	})
	addEdge(g, "region-us-east-1", "s3-assets", graph.EdgeContains)

	addNode(g, "s3-backups", "s3_bucket", "data-backup-bucket", graph.StatusActive, "region-us-east-1", "Storage", map[string]string{
		"versioning": "enabled", "encryption": "aws:kms", "public": "false",
		"lifecycle": "transition to Glacier after 30 days",
	})
	addEdge(g, "region-us-east-1", "s3-backups", graph.EdgeContains)

	// ─── CloudFront ─────────────────────────────────────
	addNode(g, "cf-distribution", "cloudfront", "cdn-distribution", graph.StatusActive, "region-us-east-1", "CDN", map[string]string{
		"domain": "d1234567890.cloudfront.net", "price_class": "PriceClass_100",
		"origins": "s3-assets, alb-web", "ssl": "ACM certificate",
	})
	addEdge(g, "region-us-east-1", "cf-distribution", graph.EdgeContains)
	addEdge(g, "cf-distribution", "s3-assets", graph.EdgeConnectsTo)
	addEdge(g, "cf-distribution", "alb-web", graph.EdgeConnectsTo)

	// ─── Route53 ────────────────────────────────────────
	addNode(g, "r53-zone", "route53", "example.com", graph.StatusActive, "region-us-east-1", "DNS", map[string]string{
		"type": "public hosted zone", "record_count": "8",
	})
	addEdge(g, "region-us-east-1", "r53-zone", graph.EdgeContains)

	addNode(g, "r53-root", "dns_record", "example.com → CDN", graph.StatusActive, "r53-zone", "DNS", map[string]string{
		"type": "A (Alias)", "target": "d1234567890.cloudfront.net",
	})
	addEdge(g, "r53-zone", "r53-root", graph.EdgeContains)
	addEdge(g, "r53-root", "cf-distribution", graph.EdgeRoutesTo)

	addNode(g, "r53-api", "dns_record", "api.example.com → ALB", graph.StatusActive, "r53-zone", "DNS", map[string]string{
		"type": "A (Alias)", "target": "web-alb-123456.us-east-1.elb.amazonaws.com",
	})
	addEdge(g, "r53-zone", "r53-api", graph.EdgeContains)
	addEdge(g, "r53-api", "alb-web", graph.EdgeRoutesTo)

	// ─── ECS Cluster ────────────────────────────────────
	addNode(g, "ecs-cluster", "ecs_cluster", "api-cluster", graph.StatusActive, "region-us-east-1", "Compute", map[string]string{
		"capacity_providers": "FARGATE, FARGATE_SPOT", "running_tasks": "5",
	})
	addEdge(g, "region-us-east-1", "ecs-cluster", graph.EdgeContains)

	// ECS Services
	ecsServices := []struct {
		id, name    string
		desired     string
		taskPrefix  string
		taskCount   int
	}{
		{"ecs-svc-api", "api-service", "3", "ecs-task-api", 3},
		{"ecs-svc-worker", "worker-service", "2", "ecs-task-worker", 2},
	}
	for _, svc := range ecsServices {
		addNode(g, svc.id, "ecs_service", svc.name, graph.StatusRunning, "ecs-cluster", "Compute", map[string]string{
			"launch_type": "FARGATE", "desired_count": svc.desired,
			"deployment": "rolling", "health_check": "/health",
		})
		addEdge(g, "ecs-cluster", svc.id, graph.EdgeContains)

		for i := 1; i <= svc.taskCount; i++ {
			taskID := fmt.Sprintf("%s-%d", svc.taskPrefix, i)
			addNode(g, taskID, "ecs_task", fmt.Sprintf("%s-task-%d", svc.name, i), graph.StatusRunning, svc.id, "Compute", map[string]string{
				"cpu": "512", "memory": "1024", "image": fmt.Sprintf("123456789.dkr.ecr.us-east-1.amazonaws.com/%s:v1.2.3", svc.name),
			})
			addEdge(g, svc.id, taskID, graph.EdgeContains)
		}
	}

	// ECS → RDS and Cache connections
	addEdge(g, "ecs-svc-api", "rds-primary", graph.EdgeConnectsTo)
	addEdge(g, "ecs-svc-api", "cache-session", graph.EdgeConnectsTo)

	// ─── Lambda ─────────────────────────────────────────
	addNode(g, "lambda-processor", "lambda", "image-processor", graph.StatusActive, "region-us-east-1", "Compute", map[string]string{
		"runtime": "python3.12", "memory": "512", "timeout": "300",
		"trigger": "S3 event (s3-assets)", "handler": "handler.process",
	})
	addEdge(g, "region-us-east-1", "lambda-processor", graph.EdgeContains)
	addEdge(g, "s3-assets", "lambda-processor", graph.EdgeConnectsTo)
	addEdge(g, "lambda-processor", "s3-backups", graph.EdgeConnectsTo)

	// ─── IAM Roles ──────────────────────────────────────
	iamRoles := []struct {
		id, name string
		meta     map[string]string
		attachTo []string
	}{
		{"iam-ecs-role", "ecs-task-role", map[string]string{
			"policies": "AmazonRDSReadOnly, AmazonS3ReadOnly, CloudWatchLogsFullAccess",
		}, []string{"ecs-svc-api", "ecs-svc-worker"}},
		{"iam-ec2-role", "ec2-instance-role", map[string]string{
			"policies": "AmazonSSMManagedInstanceCore, CloudWatchAgentServerPolicy",
		}, []string{"ec2-web-1", "ec2-web-2", "ec2-web-3", "ec2-web-4"}},
		{"iam-lambda-role", "lambda-execution-role", map[string]string{
			"policies": "AWSLambdaBasicExecutionRole, AmazonS3FullAccess",
		}, []string{"lambda-processor"}},
	}
	for _, role := range iamRoles {
		addNode(g, role.id, "iam_role", role.name, graph.StatusActive, "region-us-east-1", "Security", role.meta)
		addEdge(g, "region-us-east-1", role.id, graph.EdgeContains)
		for _, target := range role.attachTo {
			addEdge(g, role.id, target, graph.EdgeAttachedTo)
		}
	}

	// ─── CloudWatch ─────────────────────────────────────
	addNode(g, "cw-alarms", "cloudwatch", "monitoring-alarms", graph.StatusActive, "region-us-east-1", "Monitoring", map[string]string{
		"alarms": "CPU > 80%, 5xx > 10/min, RDS connections > 100",
		"dashboards": "2", "log_groups": "5",
	})
	addEdge(g, "region-us-east-1", "cw-alarms", graph.EdgeContains)

	// ─── SNS ────────────────────────────────────────────
	addNode(g, "sns-alerts", "sns", "ops-alerts", graph.StatusActive, "region-us-east-1", "Messaging", map[string]string{
		"subscriptions": "email:ops@example.com, lambda:pagerduty-handler",
	})
	addEdge(g, "region-us-east-1", "sns-alerts", graph.EdgeContains)
	addEdge(g, "cw-alarms", "sns-alerts", graph.EdgeConnectsTo)

	// ─── Intentional misconfigurations (Doctor demo) ────
	addNode(g, "s3-public", "s3_bucket", "public-website-bucket", graph.StatusActive, "region-us-east-1", "Storage", map[string]string{
		"versioning": "disabled", "encryption": "none", "public": "true",
	})
	addEdge(g, "region-us-east-1", "s3-public", graph.EdgeContains)

	addNode(g, "sg-ssh", "security_group", "legacy-ssh-sg", graph.StatusActive, "vpc-main", "Security", map[string]string{
		"inbound": "22 from 0.0.0.0/0", "outbound": "all",
	})
	addEdge(g, "vpc-main", "sg-ssh", graph.EdgeContains)
	addEdge(g, "sg-ssh", "ec2-web-1", graph.EdgeAttachedTo)

	addNode(g, "ec2-legacy", "ec2", "legacy-batch-server", graph.StatusStopped, "subnet-private-1a", "Compute", map[string]string{
		"instance_type": "t3.large", "public_ip": "54.210.1.2", "ami": "ami-0old111111111",
	})
	addEdge(g, "subnet-private-1a", "ec2-legacy", graph.EdgeContains)

	addNode(g, "ebs-unencrypted", "volume", "legacy-data-volume", graph.StatusActive, "subnet-private-1a", "Storage", map[string]string{
		"size": "200 GiB", "encrypted": "false", "type": "gp2",
	})
	addEdge(g, "subnet-private-1a", "ebs-unencrypted", graph.EdgeContains)
	addEdge(g, "ebs-unencrypted", "ec2-legacy", graph.EdgeAttachedTo)

	return g, nil
}

// ─── Helpers ────────────────────────────────────────────

func addNode(g *graph.Graph, id, typ, name, status, parent, group string, meta map[string]string) {
	g.AddNode(&graph.Node{
		ID:       id,
		Type:     typ,
		Provider: "mock",
		Name:     name,
		Status:   status,
		Region:   "us-east-1",
		Metadata: meta,
		Parent:   parent,
		Group:    group,
	})
}

func addEdge(g *graph.Graph, source, target, edgeType string) {
	id := fmt.Sprintf("%s->%s", source, target)
	g.AddEdge(&graph.Edge{
		ID:     id,
		Source: source,
		Target: target,
		Type:   edgeType,
	})
}
