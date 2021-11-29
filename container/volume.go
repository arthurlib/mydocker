package container

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"os"
	"os/exec"
	"strings"
)

// 为当前容器创建 AUFS文件系统
//Create a AUFS filesystem as container root workspace
func NewWorkSpace(volume, imageName, containerName string) {
	CreateReadOnlyLayer(imageName)             // 创建只读层（解压镜像文件）
	CreateWriteLayer(containerName)            // 创建容器可写层目录
	CreateMountPoint(containerName, imageName) // 进行aufs文件系统挂载
	if volume != "" {
		volumeURLs := strings.Split(volume, ":")
		length := len(volumeURLs)
		if length == 2 && volumeURLs[0] != "" && volumeURLs[1] != "" {
			MountVolume(volumeURLs, containerName) // 挂载到宿主机目录
			log.Infof("NewWorkSpace volume urls %q", volumeURLs)
		} else {
			log.Infof("Volume parameter input is not correct.")
		}
	}
}

// 解压镜像文件
//Decompression tar image
func CreateReadOnlyLayer(imageName string) error {
	unTarFolderUrl := RootUrl + "/" + imageName + "/" //镜像解压目录
	imageUrl := RootUrl + "/" + imageName + ".tar"    // 指定的镜像文件路径
	exist, err := PathExists(unTarFolderUrl)          // 目录是否存在
	if err != nil {
		log.Infof("Fail to judge whether dir %s exists. %v", unTarFolderUrl, err)
		return err
	}
	if !exist {
		// 创建镜像解压目录
		if err := os.MkdirAll(unTarFolderUrl, 0622); err != nil {
			log.Errorf("Mkdir %s error %v", unTarFolderUrl, err)
			return err
		}
		// 解压镜像
		if _, err := exec.Command("tar", "-xvf", imageUrl, "-C", unTarFolderUrl).CombinedOutput(); err != nil {
			log.Errorf("Untar dir %s error %v", unTarFolderUrl, err)
			return err
		}
	}
	return nil
}

// 创建容器可写层目录
func CreateWriteLayer(containerName string) {
	writeURL := fmt.Sprintf(WriteLayerUrl, containerName)
	if err := os.MkdirAll(writeURL, 0777); err != nil {
		log.Infof("Mkdir write layer dir %s error. %v", writeURL, err)
	}
}

// 挂载目录，将容器外目录挂载到容器内目录，由此可以把数据存到容器外
func MountVolume(volumeURLs []string, containerName string) error {
	parentUrl := volumeURLs[0] // 容器外目录
	if err := os.Mkdir(parentUrl, 0777); err != nil {
		log.Infof("Mkdir parent dir %s error. %v", parentUrl, err)
	}
	containerUrl := volumeURLs[1]                     //容器内目录
	mntURL := fmt.Sprintf(MntUrl, containerName)      //挂载点路径
	containerVolumeURL := mntURL + "/" + containerUrl //宿主机上的容器目录
	if err := os.Mkdir(containerVolumeURL, 0777); err != nil {
		log.Infof("Mkdir container dir %s error. %v", containerVolumeURL, err)
	}
	dirs := "dirs=" + parentUrl
	_, err := exec.Command("mount", "-t", "aufs", "-o", dirs, "none", containerVolumeURL).CombinedOutput()
	if err != nil {
		log.Errorf("Mount volume failed. %v", err)
		return err
	}
	return nil
}

// 将镜像解压目录和容器可写层目录进行 aufs文件系统挂载
func CreateMountPoint(containerName, imageName string) error {
	mntUrl := fmt.Sprintf(MntUrl, containerName)
	if err := os.MkdirAll(mntUrl, 0777); err != nil {
		log.Errorf("Mkdir mountpoint dir %s error. %v", mntUrl, err)
		return err
	}
	tmpWriteLayer := fmt.Sprintf(WriteLayerUrl, containerName) //容器可写层目录
	tmpImageLocation := RootUrl + "/" + imageName              // 镜像解压目录
	mntURL := fmt.Sprintf(MntUrl, containerName)               // 挂载点
	// 从左向右，默认第一个是可读写层，后面都是只读层
	dirs := "dirs=" + tmpWriteLayer + ":" + tmpImageLocation
	// 参考： https://www.cnblogs.com/sparkdev/p/11237347.html
	_, err := exec.Command("mount", "-t", "aufs", "-o", dirs, "none", mntURL).CombinedOutput()
	// mount aufs参考
	// https://segmentfault.com/a/1190000008489207
	// http://manpages.ubuntu.com/manpages/xenial/en/man5/aufs.5.html
	if err != nil {
		log.Errorf("Run command for creating mount point failed %v", err)
		return err
	}
	return nil
}

//Delete the AUFS filesystem while container exit
func DeleteWorkSpace(volume, containerName string) {
	if volume != "" {
		volumeURLs := strings.Split(volume, ":")
		length := len(volumeURLs)
		if length == 2 && volumeURLs[0] != "" && volumeURLs[1] != "" {
			DeleteVolume(volumeURLs, containerName)
		}
	}
	DeleteMountPoint(containerName)
	DeleteWriteLayer(containerName)
}

func DeleteMountPoint(containerName string) error {
	mntURL := fmt.Sprintf(MntUrl, containerName)
	_, err := exec.Command("umount", mntURL).CombinedOutput()
	if err != nil {
		log.Errorf("Unmount %s error %v", mntURL, err)
		return err
	}
	if err := os.RemoveAll(mntURL); err != nil {
		log.Errorf("Remove mountpoint dir %s error %v", mntURL, err)
		return err
	}
	return nil
}

func DeleteVolume(volumeURLs []string, containerName string) error {
	mntURL := fmt.Sprintf(MntUrl, containerName)
	containerUrl := mntURL + "/" + volumeURLs[1]
	if _, err := exec.Command("umount", containerUrl).CombinedOutput(); err != nil {
		log.Errorf("Umount volume %s failed. %v", containerUrl, err)
		return err
	}
	return nil
}

func DeleteWriteLayer(containerName string) {
	writeURL := fmt.Sprintf(WriteLayerUrl, containerName)
	if err := os.RemoveAll(writeURL); err != nil {
		log.Infof("Remove writeLayer dir %s error %v", writeURL, err)
	}
}

func PathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
