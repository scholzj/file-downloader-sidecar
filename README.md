# File Downloader Sidecar

[![CircleCI](https://circleci.com/gh/scholzj/file-downloader-sidecar.svg?style=svg)](https://circleci.com/gh/scholzj/file-downloader-sidecar)

This is a simple Kubernetes / OpenShift controller. The controller should be running as a sidecar container in another pod. A volume should be configured and shared between the main pod and controller containers. Controller will monitoring a single ConfigMap resource. The config map should contain a list of files and URLs where they can be downloaded. For example:
Key     | Value
------- | -------
firstFile.zip | https://download...
secondFile.txt | https://download2...

With every change in the config map, the controller will go through all files and download the new files / delete the removed files. Since the volume where the files are stored is shared by both containers, the main image will see the files and can use them.
