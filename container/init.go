package container

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// 容器初始化
func RunContainerInitProcess() error {
	// 从管道读取执行指令
	cmdArray := readUserCommand() // 例： ["/bin/sh"]  or ["ls", "-a"]  todo 验证
	if cmdArray == nil || len(cmdArray) == 0 {
		return fmt.Errorf("Run container get user command error, cmdArray is nil")
	}

	setUpMount()

	//第一个参数是可执行文件的路径，注意不会自动从PATH下面去搜索，所以：
	//1.1 要么是显式的指定全路径：/path/to/executable
	//1.2 要么是显式的指定相对路径: ./relpath/to/executable
	//1.3 要么通过exec.LookPath从PATH里面搜索出来。
	path, err := exec.LookPath(cmdArray[0]) // 在系统PATH中寻找命令的绝对路径，如果是不在系统path里的可执行文件呢？
	if err != nil {
		log.Errorf("Exec loop path error %v", err)
		return err
	}
	// syscall.Exec参考： https://www.jianshu.com/p/e1de8fc52718
	// 参考2： https://gobyexample-cn.github.io/execing-processes
	// os.Environ() 环境变量
	log.Infof("Find path %s", path)
	if err := syscall.Exec(path, cmdArray[0:], os.Environ()); err != nil {
		log.Errorf(err.Error())
	}
	return nil
}

// 获取管道进行读取
func readUserCommand() []string {
	// uintptr(3)就是指index为3的文件描述符，也就是传递进来的管道的一端
	// 一个进程默认会有三个文件描述符 stdin stdout stderr（ ls /proc/self/fd 可以看到默认的文件描述符 ）
	// cmd.ExtraFiles = []*os.File{readPipe} 设置后就成了第四个
	pipe := os.NewFile(uintptr(3), "pipe")
	defer pipe.Close()
	msg, err := ioutil.ReadAll(pipe) // 会等待
	if err != nil {
		log.Errorf("init read pipe error %v", err)
		return nil
	}
	msgStr := string(msg)
	return strings.Split(msgStr, " ")
}

/**
Init 挂载点
*/
func setUpMount() {
	pwd, err := os.Getwd() // 获取当前路径，好像读的是cmd.Dir，挂载点路径 todo 验证
	if err != nil {
		log.Errorf("Get current location error %v", err)
		return
	}
	log.Infof("Current location is %s", pwd)
	//挂载proc之前,先调用pivotRoot,把当前文件系统切换为pwd
	pivotRoot(pwd)

	//mount proc
	defaultMountFlags := syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV
	// todo
	syscall.Mount("proc", "/proc", "proc", uintptr(defaultMountFlags), "")
	// todo
	syscall.Mount("tmpfs", "/dev", "tmpfs", syscall.MS_NOSUID|syscall.MS_STRICTATIME, "mode=755")
}

//这是一个系统调用,主要功能是改变当前的root文件系统,是吧整个系统切换到一个新的root中,移除对之前root的依赖
//具体原理是把当前进程root文件系统移动到old文件夹中,使new_root成为新的root文件系统.
func pivotRoot(root string) error {
	/**
	  为了使当前root的老 root 和新 root 不在同一个文件系统下，我们把root重新mount了一次
		上行的意思是new_root和put_old不能在同一个mount namespace中
	  bind mount是把相同的内容换了一个挂载点的挂载方法
	*/
	// 这里的root重新mount解释
	// 当前进程是在一个新的命名空间中
	// 传入的root是对当前当前进程来说还是属于宿主机空间
	// 进行一下mount,就把宿主机root，变成当前进程内的root，同路径但属于不同namespace
	// 所以后面使用的root都是当前进程空间内的
	if err := syscall.Mount(root, root, "bind", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("Mount rootfs to itself error: %v", err)
	}

	// 这个新建目录是用来挂载老的根目录（/）
	// 创建 rootfs/.pivot_root 存储 old_root
	pivotDir := filepath.Join(root, ".pivot_root")
	if err := os.Mkdir(pivotDir, 0777); err != nil {
		return err
	}
	// pivot_root 到新的rootfs, 现在老的 old_root 是挂载在rootfs/.pivot_root
	// 挂载点现在依然可以在mount命令中看到
	if err := syscall.PivotRoot(root, pivotDir); err != nil {
		return fmt.Errorf("pivot_root %v", err)
	}
	// pivot_root参考： https://blog.csdn.net/linuxchyu/article/details/21109335

	// 修改当前的工作目录到根目录, 到新的根目录，需要主动切换到新的根目录
	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("chdir / %v", err)
	}

	pivotDir = filepath.Join("/", ".pivot_root")
	// umount rootfs/.pivot_root
	if err := syscall.Unmount(pivotDir, syscall.MNT_DETACH); err != nil {
		return fmt.Errorf("unmount pivot_root dir %v", err)
	}
	// 删除临时文件夹
	return os.Remove(pivotDir)
}
