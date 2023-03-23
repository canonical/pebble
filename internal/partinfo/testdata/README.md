# `partinfo` test data

This directory contains test data for the `partinfo` package.
Instead of having the CI generate the test data on the fly or uploading large
blobs of binary data into the repository, we include the superblocks from
valid Ext4/VFAT disk images.

## Generating ext4 superblocks
```shell
truncate ext4.img -s 4M && mkfs.ext4 -L "My label" ext4.img
head -c 2048 ext4.img > ext4-superblock-labelled.img && rm ext4.img
```

## Generating VFAT superblocks
```shell
truncate vfat.img -s 64M && mkfs.vfat -F32 -n "My label" vfat.img
head -c 512 vfat.img > vfat-superblock-labelled.img && rm vfat.img
```

## Generating random blobs
```shell
dd if=/dev/random of=garbage-8.bin bs=8 count=1
dd if=/dev/random of=garbage-2k.bin bs=2048 count=1
```