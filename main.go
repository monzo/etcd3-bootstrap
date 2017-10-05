package main

import (
	"flag"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

var (
	ebsVolumeName string
	mountPoint    string
	blockDevice   string
	awsRegion     string
)

func init() {
	flag.StringVar(&awsRegion, "aws-region", "eu-west-1", "AWS region this instance is on")
	flag.StringVar(&ebsVolumeName, "ebs-volume-name", "", "EBS volume to attach to this node")
	flag.StringVar(&mountPoint, "mount-point", "/var/lib/etcd-bootstrap", "EBS volume mount point")
	flag.StringVar(&blockDevice, "block-device", "/dev/xvdf", "Block device to attach as")
	flag.Parse()
}

func main() {
	// Initialize AWS session
	awsSession := session.Must(session.NewSession())

	// Create ec2 and metadata svc clients with specified region
	ec2SVC := ec2.New(awsSession, aws.NewConfig().WithRegion(awsRegion))
	metadataSVC := ec2metadata.New(awsSession, aws.NewConfig().WithRegion(awsRegion))

	// obtain current AZ, required for finding volume
	availabilityZone, err := metadataSVC.GetMetadata("placement/availability-zone")
	if err != nil {
		panic(err)
	}

	volume, err := volumeFromName(ec2SVC, ebsVolumeName, availabilityZone)
	if err != nil {
		panic(err)
	}

	instanceID, err := metadataSVC.GetMetadata("instance-id")
	if err != nil {
		panic(err)
	}

	err = attachVolume(ec2SVC, instanceID, volume)
	if err != nil {
		panic(err)
	}

	if ok := volumeInitialized(blockDevice); !ok {
		log.Println("NEED TO FORMAT")
	}

}
