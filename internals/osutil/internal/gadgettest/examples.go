// Copyright (C) 2022 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package gadgettest

import (
	"github.com/canonical/pebble/internals/osutil/disks"
	"github.com/canonical/pebble/internals/osutil/internal/quantity"
)

const oneMeg = uint64(quantity.SizeMiB)

var VMSystemVolumeDiskMappingSeedFsLabelCaps = &disks.FakeDiskMapping{
	DevNode: "/dev/vda",
	DevPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda",
	DevNum:  "600:1",
	// assume 34 sectors at end for GPT headers backup
	DiskUsableSectorEnd: 5120*oneMeg/512 - 34,
	DiskSizeInBytes:     5120 * oneMeg,
	SectorSizeBytes:     512,
	DiskSchema:          "gpt",
	ID:                  "f0eef013-a777-4a27-aaf0-dbb5cf68c2b6",
	Structure: []disks.Partition{
		{
			KernelDeviceNode: "/dev/vda1",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda2",
			PartitionUUID:    "4b436628-71ba-43f9-aa12-76b84fe32728",
			PartitionLabel:   "ubuntu-seed",
			PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			FilesystemUUID:   "04D6-5AE2",
			FilesystemLabel:  "UBUNTU-SEED",
			FilesystemType:   "vfat",
			StartInBytes:     1 * oneMeg,
			SizeInBytes:      1200 * oneMeg,
			Major:            600,
			Minor:            3,
			DiskIndex:        1,
		},
		{
			KernelDeviceNode: "/dev/vda2",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda3",
			PartitionUUID:    "ade3ba65-7831-fd40-bbe2-e01c9774ed5b",
			PartitionLabel:   "ubuntu-boot",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:   "5b3e775a-407d-4af7-aa16-b92a8b7507e6",
			FilesystemLabel:  "ubuntu-boot",
			FilesystemType:   "ext4",
			StartInBytes:     (1200 + 1) * oneMeg,
			SizeInBytes:      750 * oneMeg,
			Major:            600,
			Minor:            4,
			DiskIndex:        2,
		},
		{
			KernelDeviceNode: "/dev/vda3",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda4",
			PartitionUUID:    "f1d01870-194b-8a45-84c0-0d1c90e17d9d",
			PartitionLabel:   "ubuntu-save",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:   "6766b605-9cd5-47ae-bc48-807c778b9987",
			FilesystemLabel:  "ubuntu-save",
			FilesystemType:   "ext4",
			StartInBytes:     (1200 + 1 + 750) * oneMeg,
			SizeInBytes:      16 * oneMeg,
			Major:            600,
			Minor:            5,
			DiskIndex:        3,
		},
		{
			KernelDeviceNode: "/dev/vda4",
			KernelDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda5",
			PartitionUUID:    "4994f0e5-1ead-1a4d-b696-2d8cb1fa980d",
			PartitionLabel:   "ubuntu-data",
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			FilesystemUUID:   "4e29a1e9-526d-48fc-a5c2-4f97e7e011e2",
			FilesystemLabel:  "ubuntu-data",
			FilesystemType:   "ext4",
			StartInBytes:     (1200 + 1 + 750 + 16) * oneMeg,
			// including the last usable sector - the offset
			SizeInBytes: ((5120*oneMeg - 33*512) - (1+1+1200+750+16)*oneMeg),
			Major:       600,
			Minor:       6,
			DiskIndex:   4,
		},
	},
}
