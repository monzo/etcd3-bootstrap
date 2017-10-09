package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/pkg/errors"
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

	for {
		volumeDescs, err := svc.DescribeVolumes(&ec2.DescribeVolumesInput{
			VolumeIds: []*string{volume.VolumeId},
		})
		if err != nil {
			return errors.Wrap(err, "Error retrieving volume description status")
		}

		volumes := volumeDescs.Volumes
		if len(volumes) == 0 {
			continue
		}

		if len(volumes[0].Attachments) == 0 {
			continue
		}

		if *volumes[0].Attachments[0].State == ec2.VolumeAttachmentStateAttached {
			break
		}

		log.Printf("Waiting for attachment to complete. Current state: %s", *volumes[0].Attachments[0].State)
		time.Sleep(1 * time.Second)
	}

	log.Printf("Attached volume %s to instance %s as device %s\n",
		*volume.VolumeId, instanceID, blockDevice)

	return nil
}

func ensureVolumeInited(blockDevice string) error {
	log.Printf("Checking for existing ext4 filesystem on device: %s\n", blockDevice)

	out, err := exec.Command("/usr/sbin/blkid", blockDevice).Output()
	if err != nil {
		return errors.Wrap(err, "blkid stdout pipe failed")
	}

	log.Println(string(out))
	if strings.Contains(string(out), "ext4") {
		log.Println("Found existing ext4 filesystem")
		return nil
	}

	log.Println("Filesystem not present")

	// format volume here
	cmd := exec.Command("sudo", "/usr/sbin/mkfs.ext4", blockDevice, "-F")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.Wrap(err, "mkfs.ext4 failed")
	}

	return nil
}

func mountVolume(blockDevice, mountPoint string) error {
	log.Printf("Mounting device %s at %s\n", blockDevice, mountPoint)

	// ensure mount point exists
	cmd := exec.Command("sudo", "mkdir", "-p", mountPoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.Wrap(err, "mountpoint creation failed")
	}

	cmd = exec.Command("sudo", "mount", blockDevice, mountPoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.Wrap(err, "mount failed")
	}

	log.Printf("Device %s successfully mounted at %s\n", blockDevice, mountPoint)

	return nil
}
