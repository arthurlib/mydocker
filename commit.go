package main

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/xianlubird/mydocker/container"
	"os/exec"
)

// 通过容器构建新镜像
func commitContainer(containerName, imageName string) {
	mntURL := fmt.Sprintf(container.MntUrl, containerName)
	mntURL += "/"

	imageTar := container.RootUrl + "/" + imageName + ".tar"

	// 直接将挂载点目录进行打包，就成了新镜像（docker的镜像是分层(layer)的）
	if _, err := exec.Command("tar", "-czf", imageTar, "-C", mntURL, ".").CombinedOutput(); err != nil {
		log.Errorf("Tar folder %s error %v", mntURL, err)
	}
}
