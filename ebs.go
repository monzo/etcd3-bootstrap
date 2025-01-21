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
	"github.com/jpillora/backoff"
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

	b := &backoff.Backoff{
		Min:    100 * time.Millisecond,
		Max:    100 * time.Second,
		Factor: 2,
		Jitter: false,
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
		b.Duration()
	}

	log.Printf("Attached volume %s to instance %s as device %s\n",
		*volume.VolumeId, instanceID, blockDevice)

	return nil
}

func ensureVolumeInited(blockDevice, fileSystemFormatType, fileSystemFormatArguments string) error {
	log.Printf("Checking for existing filesystem on device: %s\n", blockDevice)

	if err := exec.Command("sudo", "/usr/sbin/blkid", blockDevice).Run(); err == nil {
		log.Println("Found existing filesystem")
		return nil
	}

	log.Println("Filesystem not present")

	// format volume here
	cmd := exec.Command("sudo", "/usr/sbin/mkfs."+fileSystemFormatType, blockDevice, fileSystemFormatArguments)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.Wrap(err, "mkfs."+fileSystemFormatType+" failed")
	}

	return nil
}

func ensureVolumeMounted(blockDevice, mountPoint string) error {
	log.Printf("Mounting device %s at %s\n", blockDevice, mountPoint)

	// ensure mount point exists
	cmd := exec.Command("sudo", "mkdir", "-p", mountPoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return errors.Wrap(err, "mountpoint creation failed")
	}

	cmd = exec.Command("sudo", "mount", blockDevice, mountPoint)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err == nil {
		log.Printf("Device %s successfully mounted at %s\n", blockDevice, mountPoint)
		return nil
	}

	// mount failed, double-check as this may result from a previous mount
	log.Println("Mount failed. perhaps already mounted, will double check")
	out, err := exec.Command("mount").Output()
	if err != nil {
		return errors.Wrap(err, "cannot mount or verify mount. cowardly refusing to continue")
	}

	if strings.Contains(string(out), fmt.Sprintf("%s on %s", blockDevice, mountPoint)) {
		log.Printf("Device %s successfully mounted at %s\n", blockDevice, mountPoint)
		return nil
	}

	return errors.New("cannot mount or verify mount. cowardly refusing to continue")
}

func ensureVolumeWriteable(mountPoint string) error {
	log.Printf("Ensuring %s is r/w by etcd\n", mountPoint)
	if err := exec.Command("sudo", "/usr/bin/chown", "-R", "etcd:etcd", mountPoint).Run(); err != nil {
		return errors.Wrapf(err, "cannot make %s writeable by etcd", mountPoint)
	}

	return nil
}
