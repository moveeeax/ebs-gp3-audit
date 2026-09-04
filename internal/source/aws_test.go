package source

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type fakeEC2 struct {
	pages []*ec2.DescribeVolumesOutput
	call  int
}

func (f *fakeEC2) DescribeVolumes(_ context.Context, _ *ec2.DescribeVolumesInput, _ ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	out := f.pages[f.call]
	f.call++
	return out, nil
}

func TestVolumesFromPaginatesAndNormalizes(t *testing.T) {
	next := "tok"
	api := &fakeEC2{pages: []*ec2.DescribeVolumesOutput{
		{
			NextToken: &next,
			Volumes: []ec2types.Volume{
				{VolumeId: aws.String("vol-1"), VolumeType: ec2types.VolumeTypeGp2, Size: aws.Int32(500)},
			},
		},
		{
			Volumes: []ec2types.Volume{
				{VolumeId: aws.String("vol-2"), VolumeType: ec2types.VolumeTypeGp3, Size: aws.Int32(100), Iops: aws.Int32(3000), Throughput: aws.Int32(125), AvailabilityZone: aws.String("us-east-1a")},
			},
		},
	}}

	vols, err := volumesFrom(context.Background(), api, "us-east-1")
	if err != nil {
		t.Fatalf("volumesFrom: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("want 2 volumes across pages, got %d", len(vols))
	}
	if vols[0].ID != "vol-1" || vols[0].Type != "gp2" || vols[0].SizeGiB != 500 {
		t.Fatalf("first volume normalized wrong: %+v", vols[0])
	}
	if vols[1].Throughput != 125 || vols[1].AZ != "us-east-1a" || vols[1].Region != "us-east-1" {
		t.Fatalf("second volume normalized wrong: %+v", vols[1])
	}
}
