package container

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"os"
	"os/exec"
	"syscall"
)

var (
	RUNNING             string = "running"
	STOP                string = "stopped"
	Exit                string = "exited"
	DefaultInfoLocation string = "/var/run/mydocker/%s/" // 容器信息存放目录
	ConfigName          string = "config.json"           // 容器基本信息文件
	ContainerLogFile    string = "container.log"         // 容器日志文件
	RootUrl             string = "/root"
	MntUrl              string = "/root/mnt/%s"        // 挂载点 （cd /mnt/name就可以进入被挂载的目录）
	WriteLayerUrl       string = "/root/writeLayer/%s" // 容器可写层存放目录
)

// 容器基本信息
type ContainerInfo struct {
	Pid         string   `json:"pid"`         //容器的init进程在宿主机上的 PID
	Id          string   `json:"id"`          //容器Id
	Name        string   `json:"name"`        //容器名
	Command     string   `json:"command"`     //容器内init运行命令
	CreatedTime string   `json:"createTime"`  //创建时间
	Status      string   `json:"status"`      //容器的状态
	Volume      string   `json:"volume"`      //容器的数据卷
	PortMapping []string `json:"portmapping"` //端口映射
}

// 创建容器进程
func NewParentProcess(tty bool, containerName, volume, imageName string, envSlice []string) (*exec.Cmd, *os.File) {
	// 创建管道
	readPipe, writePipe, err := NewPipe()
	if err != nil {
		log.Errorf("New pipe error %v", err)
		return nil, nil
	}

	// 获取自身进程
	//1.这里的/proc/self/exe 调用，其中/proc/self指的是当前运行进程自己的环境，exec其实就是自己调用了自己，我们使用这种方式实现对创建出来的进程进行初始化
	initCmd, err := os.Readlink("/proc/self/exe") // 代表当前程序
	if err != nil {
		log.Errorf("get init process error %v", err)
		return nil, nil
	}

	//2.后面args是参数，其中 init 是传递给本进程的第一个参数，这在本例子中，其实就是会去调用我们的 initCommand 去初始化进程的一些环境和资源
	cmd := exec.Command(initCmd, "init") // 这是容器进程的init
	//3. 下面的clone 参数就是去 fork 出来的一个新进程，并且使用了namespace隔离新创建的进程和外部的环境。
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS |
			syscall.CLONE_NEWNET | syscall.CLONE_NEWIPC,
	}

	if tty {
		//4. 如果用户指定了-ti 参数，我们就需要把当前进程的输入输出导入到标准输入输出上
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		dirURL := fmt.Sprintf(DefaultInfoLocation, containerName)
		// 创建目录
		if err := os.MkdirAll(dirURL, 0622); err != nil {
			log.Errorf("NewParentProcess mkdir %s error %v", dirURL, err)
			return nil, nil
		}

		stdLogFilePath := dirURL + ContainerLogFile  // 容器日志文件路径
		stdLogFile, err := os.Create(stdLogFilePath) // 创建文件
		if err != nil {
			log.Errorf("NewParentProcess create file %s error %v", stdLogFilePath, err)
			return nil, nil
		}
		cmd.Stdout = stdLogFile // 重定向标准输入到日志文件
	}

	cmd.ExtraFiles = []*os.File{readPipe}          // 设置读管道
	cmd.Env = append(os.Environ(), envSlice...)    // 给新进程设置环境变量
	NewWorkSpace(volume, imageName, containerName) // 为当前容器创建 AUFS文件系统，挂载目录
	cmd.Dir = fmt.Sprintf(MntUrl, containerName)   // 挂载点路径
	return cmd, writePipe
}

// 管道参考： https://blog.schwarzeni.com/2020/07/05/Golang-%E4%B8%AD%E7%9A%84-Pipe-%E4%BD%BF%E7%94%A8/
func NewPipe() (*os.File, *os.File, error) {
	read, write, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	return read, write, nil
}
