package source

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/moveeeax/ebs-gp3-audit/internal/audit"
)

// describeVolumesAPI is the read-only slice of the EC2 API this tool needs.
type describeVolumesAPI interface {
	DescribeVolumes(ctx context.Context, in *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error)
}

// FromAWS enumerates every EBS volume in the ambient account/region using only
// ec2:DescribeVolumes. It never mutates anything.
func FromAWS(ctx context.Context, region string) ([]audit.Volume, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return volumesFrom(ctx, ec2.NewFromConfig(cfg), cfg.Region)
}

// volumesFrom paginates DescribeVolumes and normalizes the results. It is split
// out so tests can inject a fake API without touching AWS.
func volumesFrom(ctx context.Context, api describeVolumesAPI, region string) ([]audit.Volume, error) {
	var out []audit.Volume
	p := ec2.NewDescribeVolumesPaginator(api, &ec2.DescribeVolumesInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("describe volumes: %w", err)
		}
		for _, v := range page.Volumes {
			out = append(out, normalize(v, region))
		}
	}
	return out, nil
}

func normalize(v ec2types.Volume, region string) audit.Volume {
	nv := audit.Volume{
		ID:      aws.ToString(v.VolumeId),
		Type:    string(v.VolumeType),
		SizeGiB: int64(aws.ToInt32(v.Size)),
		IOPS:    int64(aws.ToInt32(v.Iops)),
		Region:  region,
	}
	if v.Throughput != nil {
		nv.Throughput = int64(aws.ToInt32(v.Throughput))
	}
	if v.AvailabilityZone != nil {
		nv.AZ = aws.ToString(v.AvailabilityZone)
	}
	return nv
}
