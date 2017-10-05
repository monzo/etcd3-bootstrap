package main

import (
	"bufio"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func volumeFromName(svc *ec2.EC2, volumeName, az string) (*ec2.Volume, error) {
	input := &ec2.DescribeVolumesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []*string{aws.String(volumeName)},
			},
			{
				Name:   aws.String("availability-zone"),
				Values: []*string{aws.String(az)},
			},
		},
	}

	result, err := svc.DescribeVolumes(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			return nil, aerr
		}
		return nil, err
	}

	if len(result.Volumes) == 0 {
		return nil, fmt.Errorf("cannot find volume-id with name: %s", volumeName)
	}

	log.Printf("Resolved volume %s to %s\n", volumeName, *result.Volumes[0].VolumeId)

	return result.Volumes[0], nil
}

func attachVolume(svc *ec2.EC2, instanceID string, volume *ec2.Volume) error {
	log.Printf("Will attach volume %s to instance id %s\n", *volume.VolumeId, instanceID)

	// check if volume is already attached to this instance (ie, reboot)
	if len(volume.Attachments) > 0 && *volume.Attachments[0].InstanceId == instanceID {
		log.Printf("Volume %s is already attached to instance %s as device %s\n",
			*volume.VolumeId, instanceID, *volume.Attachments[0].Device)
		return nil
	}

	input := &ec2.AttachVolumeInput{
		Device:     aws.String(blockDevice),
		InstanceId: aws.String(instanceID),
		VolumeId:   volume.VolumeId,
	}

	_, err := svc.AttachVolume(input)
	if err != nil {
		if aerr, ok := err.(awserr.Error); ok {
			return aerr
		}
		return err
	}

	log.Printf("Attached volume %s to instance %s as device %s\n",
		*volume.VolumeId, instanceID, blockDevice)

	return nil
}

func volumeInitialized(blockDevice string) bool {
	cmd := exec.Command("blkid", blockDevice)
	cmdReader, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}

	scanner := bufio.NewScanner(cmdReader)
	go func() {
		if err := cmd.Run(); err != nil {
			panic(err)
		}
	}()

	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "ext4") {
			return true
		}
	}

	return false
}
