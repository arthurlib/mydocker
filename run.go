package main

import (
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/xianlubird/mydocker/cgroups"
	"github.com/xianlubird/mydocker/cgroups/subsystems"
	"github.com/xianlubird/mydocker/container"
	"github.com/xianlubird/mydocker/network"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"
)

/*
createTty       是否前台运行
cmdArray        需要执行的指令
resConf         资源限制设置
containerName   指定创建的容器名字
volume          挂载信息
imageName       指定镜像文件名字
envSlice        环境变量
network         网络配置信息
portmapping     端口映射
*/
func Run(tty bool, comArray []string, res *subsystems.ResourceConfig, containerName, volume, imageName string,
	envSlice []string, nw string, portmapping []string) {
	// 如果容器名字没有指定，则使用随机数
	containerID := randStringBytes(10)
	if containerName == "" {
		containerName = containerID
	}
	// 创建容器进程
	parent, writePipe := container.NewParentProcess(tty, containerName, volume, imageName, envSlice)
	if parent == nil {
		log.Errorf("New parent process error")
		return
	}

	// 实际启动容器进程，进行了初始化
	if err := parent.Start(); err != nil {
		log.Error(err)
	}

	//record container info
	// 记录容器信息，例如 ps读取容器信息
	containerName, err := recordContainerInfo(parent.Process.Pid, comArray, containerName, containerID, volume)
	if err != nil {
		log.Errorf("Record container info error %v", err)
		return
	}

	// 资源限制逻辑
	// use containerID as cgroup name
	cgroupManager := cgroups.NewCgroupManager(containerID)
	defer cgroupManager.Destroy()
	cgroupManager.Set(res)                  // 创建子cgroup，并写入限制数值
	cgroupManager.Apply(parent.Process.Pid) // 生效，把容器进程id写入对应的tasks文件

	// 配置网络信息
	if nw != "" {
		// config container network
		network.Init() // 初始化网络配置
		containerInfo := &container.ContainerInfo{
			Id:          containerID,
			Pid:         strconv.Itoa(parent.Process.Pid),
			Name:        containerName,
			PortMapping: portmapping,
		}
		// 连接到网络
		if err := network.Connect(nw, containerInfo); err != nil {
			log.Errorf("Error Connect Network %v", err)
			return
		}
	}

	// 最终执行指令
	sendInitCommand(comArray, writePipe)

	//如果 detach 创建了容器 就不能再去等待，创建容器之后 父进程就已经退出了。
	//因此 这里只是将容器内的 init 进程启动起来 就己经完成工作，
	//紧接着就可以退出，然后由操作系统进程 ID为1 的init 进程去接管容器进程。
	if tty { // 如果设置了前台运行
		parent.Wait()
		deleteContainerInfo(containerName)               // 删除容器信息
		container.DeleteWorkSpace(volume, containerName) // 删除NewWorkSpace创建的工作空间
	}

}

// 将指令通过管道发给容器进进程
func sendInitCommand(comArray []string, writePipe *os.File) {
	command := strings.Join(comArray, " ")
	log.Infof("command all is %s", command)
	writePipe.WriteString(command) // 将指令通过管道发给容器进进程
	writePipe.Close()
}

// 记录容器信息，例如 ps读取容器信息
func recordContainerInfo(containerPID int, commandArray []string, containerName, id, volume string) (string, error) {
	/*
		containerPID: 容器进程id
		commandArray: 执行的指令
		containerName: 容器名字
		id: 容器id,是一个随机数
		volume: 挂载信息
	*/
	createTime := time.Now().Format("2006-01-02 15:04:05")
	command := strings.Join(commandArray, "")
	containerInfo := &container.ContainerInfo{
		Id:          id,
		Pid:         strconv.Itoa(containerPID),
		Command:     command,
		CreatedTime: createTime,
		Status:      container.RUNNING,
		Name:        containerName,
		Volume:      volume,
	}

	jsonBytes, err := json.Marshal(containerInfo)
	if err != nil {
		log.Errorf("Record container info error %v", err)
		return "", err
	}
	jsonStr := string(jsonBytes) // 将容器信息序列化

	dirUrl := fmt.Sprintf(container.DefaultInfoLocation, containerName) // 存放容器信息的路径
	if err := os.MkdirAll(dirUrl, 0622); err != nil {
		log.Errorf("Mkdir error %s error %v", dirUrl, err)
		return "", err
	}
	// 创建存放容器信息的文件，并写入
	fileName := dirUrl + "/" + container.ConfigName
	file, err := os.Create(fileName)
	defer file.Close()
	if err != nil {
		log.Errorf("Create file %s error %v", fileName, err)
		return "", err
	}
	if _, err := file.WriteString(jsonStr); err != nil {
		log.Errorf("File write string error %v", err)
		return "", err
	}

	return containerName, nil
}

func deleteContainerInfo(containerId string) {
	dirURL := fmt.Sprintf(container.DefaultInfoLocation, containerId)
	if err := os.RemoveAll(dirURL); err != nil {
		log.Errorf("Remove dir %s error %v", dirURL, err)
	}
}

func randStringBytes(n int) string {
	letterBytes := "1234567890"
	rand.Seed(time.Now().UnixNano())
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}
