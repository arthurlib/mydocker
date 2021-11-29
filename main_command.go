package main

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/urfave/cli"
	"github.com/xianlubird/mydocker/cgroups/subsystems"
	"github.com/xianlubird/mydocker/container"
	"github.com/xianlubird/mydocker/network"
	"os"
)

var runCommand = cli.Command{
	Name:  "run",
	Usage: `Create a container with namespace and cgroups limit ie: mydocker run -ti [image] [command]`,
	Flags: []cli.Flag{
		cli.BoolFlag{ // 是否前台运行命令行
			Name:  "ti",
			Usage: "enable tty",
		},
		cli.BoolFlag{ // 是否后台运行
			Name:  "d",
			Usage: "detach container",
		},
		cli.StringFlag{ // 添加内存限制
			Name:  "m",
			Usage: "memory limit",
		},
		// cpu设置参考 https://www.cnblogs.com/charlieroro/p/10281469.html
		cli.StringFlag{ // 相对比例限制cgroup的cpu
			Name:  "cpushare",
			Usage: "cpushare limit",
		},
		cli.StringFlag{ //
			Name:  "cpuset",
			Usage: "cpuset limit",
		},
		cli.StringFlag{ // 设置容器名字
			Name:  "name",
			Usage: "container name",
		},
		cli.StringFlag{ // 设置挂载目录
			Name:  "v",
			Usage: "volume",
		},
		cli.StringSliceFlag{ // 设置环境变量
			Name:  "e",
			Usage: "set environment",
		},
		cli.StringFlag{ // 设置容器网络配置   mydocker run -ti -p 80:80 --net testbridgenet xxxx
			Name:  "net",
			Usage: "container network",
		},
		cli.StringSliceFlag{ // 设置端口映射
			Name:  "p",
			Usage: "port mapping",
		},
	},
	Action: func(context *cli.Context) error {
		if len(context.Args()) < 1 { //没有传入参数直接返回
			return fmt.Errorf("Missing container command")
		}
		//读取命令行参数
		var cmdArray []string
		for _, arg := range context.Args() {
			cmdArray = append(cmdArray, arg)
		}

		//get image name
		imageName := cmdArray[0] // 指定镜像文件名字
		cmdArray = cmdArray[1:]  // 需要执行的指令

		createTty := context.Bool("ti") // 是否前台运行
		detach := context.Bool("d")     // 是否后台运行
		// 不能同时指定
		if createTty && detach {
			return fmt.Errorf("ti and d paramter can not both provided")
		}
		// 资源限制设置
		resConf := &subsystems.ResourceConfig{
			MemoryLimit: context.String("m"),
			CpuSet:      context.String("cpuset"),
			CpuShare:    context.String("cpushare"),
		}
		log.Infof("createTty %v", createTty)
		containerName := context.String("name") // 指定创建的容器名字
		volume := context.String("v")           // 挂载信息，挂载后可以使用宿主机的目录，删除容器后可以保留数据
		network := context.String("net")        // 网络配置信息

		envSlice := context.StringSlice("e")    // 环境变量
		portmapping := context.StringSlice("p") // 端口映射

		Run(createTty, cmdArray, resConf, containerName, volume, imageName, envSlice, network, portmapping)
		return nil
	},
}

var initCommand = cli.Command{
	Name:  "init",
	Usage: "Init container process run user's process in container. Do not call it outside",
	Action: func(context *cli.Context) error {
		log.Infof("init come on")
		err := container.RunContainerInitProcess()
		return err
	},
}

var listCommand = cli.Command{
	Name:  "ps",
	Usage: "list all the containers",
	Action: func(context *cli.Context) error {
		ListContainers()
		return nil
	},
}

var logCommand = cli.Command{
	Name:  "logs",
	Usage: "print logs of a container",
	Action: func(context *cli.Context) error {
		if len(context.Args()) < 1 {
			return fmt.Errorf("Please input your container name")
		}
		containerName := context.Args().Get(0)
		logContainer(containerName)
		return nil
	},
}

var execCommand = cli.Command{
	Name:  "exec",
	Usage: "exec a command into container",
	Action: func(context *cli.Context) error {
		//This is for callback
		if os.Getenv(ENV_EXEC_PID) != "" {
			log.Infof("pid callback pid %s", os.Getgid())
			return nil
		}
		// mydocker exec 容器名 命令
		if len(context.Args()) < 2 {
			return fmt.Errorf("Missing container name or command")
		}
		containerName := context.Args().Get(0) // 容器名
		var commandArray []string
		for _, arg := range context.Args().Tail() { // 指令列表
			commandArray = append(commandArray, arg)
		}
		ExecContainer(containerName, commandArray)
		return nil
	},
}

var stopCommand = cli.Command{
	Name:  "stop",
	Usage: "stop a container",
	Action: func(context *cli.Context) error {
		if len(context.Args()) < 1 {
			return fmt.Errorf("Missing container name")
		}
		containerName := context.Args().Get(0)
		stopContainer(containerName)
		return nil
	},
}

var removeCommand = cli.Command{
	Name:  "rm",
	Usage: "remove unused containers",
	Action: func(context *cli.Context) error {
		if len(context.Args()) < 1 {
			return fmt.Errorf("Missing container name")
		}
		containerName := context.Args().Get(0)
		removeContainer(containerName)
		return nil
	},
}

var commitCommand = cli.Command{
	Name:  "commit",
	Usage: "commit a container into image",
	Action: func(context *cli.Context) error {
		if len(context.Args()) < 2 {
			return fmt.Errorf("Missing container name and image name")
		}
		containerName := context.Args().Get(0)
		imageName := context.Args().Get(1)
		commitContainer(containerName, imageName) // 通过容器构建新镜像
		return nil
	},
}

var networkCommand = cli.Command{
	Name:  "network",
	Usage: "container network commands",
	Subcommands: []cli.Command{
		{
			Name:  "create",
			Usage: "create a container network",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "driver",
					Usage: "network driver",
				},
				cli.StringFlag{
					Name:  "subnet",
					Usage: "subnet cidr",
				},
			},
			Action: func(context *cli.Context) error {
				// mydocker network create --subnet 192.168.0.0/24 --driver bridge testbridgenet
				if len(context.Args()) < 1 {
					return fmt.Errorf("Missing network name")
				}
				// 网络初始化，创建网络驱动，读取已存在网络配置
				network.Init()
				// 创建网络
				err := network.CreateNetwork(context.String("driver"), context.String("subnet"), context.Args()[0])
				if err != nil {
					return fmt.Errorf("create network error: %+v", err)
				}
				return nil
			},
		},
		{
			Name:  "list",
			Usage: "list container network",
			Action: func(context *cli.Context) error {
				network.Init()
				// 列出网络信息
				network.ListNetwork()
				return nil
			},
		},
		{
			Name:  "remove",
			Usage: "remove container network",
			Action: func(context *cli.Context) error {
				if len(context.Args()) < 1 {
					return fmt.Errorf("Missing network name")
				}
				network.Init()
				err := network.DeleteNetwork(context.Args()[0])
				if err != nil {
					return fmt.Errorf("remove network error: %+v", err)
				}
				return nil
			},
		},
	},
}
