package network

// "github.com/vishvananda/netlink"  // 操作网络接口，路由表配置的库，相当于通过ip命令去管理网络接口
// "net"  // 主要用到这个包中所定义的网路地址的数据接口和对网络地址的处理
// "github.com/vishvananda/netns"  // 进出Net Namespace的库，可以让netlink库中配置网络接口的代码在某个容器的Net Namespace中执行

import (
	"fmt"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"net"
	//"os"
	"encoding/json"
	"github.com/Sirupsen/logrus"
	"github.com/xianlubird/mydocker/container"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
)

var (
	defaultNetworkPath = "/var/run/mydocker/network/network/"
	drivers            = map[string]NetworkDriver{}
	networks           = map[string]*Network{}
)

// 网络端点
// 包括连接到网络的一些信息， 比如地址Veth设备，端口映射，连接的容器和网络等信息
// 网络端点信息传输需要靠网络功能的两个组件配合完成，这两个组件分别为网络驱动（NetworkDriver）和IPAM
type Endpoint struct {
	ID          string           `json:"id"`
	Device      netlink.Veth     `json:"dev"`
	IPAddress   net.IP           `json:"ip"`
	MacAddress  net.HardwareAddr `json:"mac"`
	Network     *Network
	PortMapping []string
}

// 网络信息
type Network struct {
	Name    string     // 网络名
	IpRange *net.IPNet // 地址段
	Driver  string     // 网络驱动名
}

// IPAM说明
// IPAM 也是网络功能中的一个组件，用于网络 IP 地址的分配和释放，包括容器的 IP 地址和网络网关的ip地址。下一节会具体介绍IPAM的实现，它的主要功能如下。
//* IPAM.Allocate(subnet *net.IPNet）从指定的 subnet 网段中分配 IP 地址
//* IPAM.Release(subnet net.IPNet, ipaddr net.IP）从指定的 subnet 网段中释放掉指定的 IP 地址

// 网络驱动
type NetworkDriver interface {
	// 驱动名
	Name() string
	// 创建网络
	Create(subnet string, name string) (*Network, error)
	// 删除网络
	Delete(network Network) error
	// 连接容器网络端点到网络
	Connect(network *Network, endpoint *Endpoint) error
	// 从网络上一处容器网络端点
	Disconnect(network Network, endpoint *Endpoint) error
}

// 保存网络信息到文件
func (nw *Network) dump(dumpPath string) error {
	if _, err := os.Stat(dumpPath); err != nil {
		if os.IsNotExist(err) {
			os.MkdirAll(dumpPath, 0644)
		} else {
			return err
		}
	}

	nwPath := path.Join(dumpPath, nw.Name)
	nwFile, err := os.OpenFile(nwPath, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		logrus.Errorf("error：", err)
		return err
	}
	defer nwFile.Close()

	nwJson, err := json.Marshal(nw)
	if err != nil {
		logrus.Errorf("error：", err)
		return err
	}

	_, err = nwFile.Write(nwJson)
	if err != nil {
		logrus.Errorf("error：", err)
		return err
	}
	return nil
}

// 删除网络配置文件
func (nw *Network) remove(dumpPath string) error {
	if _, err := os.Stat(path.Join(dumpPath, nw.Name)); err != nil {
		if os.IsNotExist(err) {
			return nil
		} else {
			return err
		}
	} else {
		return os.Remove(path.Join(dumpPath, nw.Name))
	}
}

// 读取网络信息
func (nw *Network) load(dumpPath string) error {
	nwConfigFile, err := os.Open(dumpPath) // 网络信息文件路径
	defer nwConfigFile.Close()
	if err != nil {
		return err
	}
	nwJson := make([]byte, 2000)
	n, err := nwConfigFile.Read(nwJson)
	if err != nil {
		return err
	}

	err = json.Unmarshal(nwJson[:n], nw)
	if err != nil {
		logrus.Errorf("Error load nw info", err)
		return err
	}
	return nil
}

// 网络初始化
func Init() error {
	var bridgeDriver = BridgeNetworkDriver{}     // 初始化一个bridge的网络驱动
	drivers[bridgeDriver.Name()] = &bridgeDriver // 存入全局变量中

	// 创建网络配置存放目录
	if _, err := os.Stat(defaultNetworkPath); err != nil {
		if os.IsNotExist(err) {
			os.MkdirAll(defaultNetworkPath, 0644)
		} else {
			return err
		}
	}

	filepath.Walk(defaultNetworkPath, func(nwPath string, info os.FileInfo, err error) error {
		if strings.HasSuffix(nwPath, "/") {
			return nil
		}
		_, nwName := path.Split(nwPath) // 文件名作为网络名
		nw := &Network{                 // 网络信息
			Name: nwName,
		}

		// 读取网络配置信息
		if err := nw.load(nwPath); err != nil {
			logrus.Errorf("error load network: %s", err)
		}

		networks[nwName] = nw // 存到全局变量中
		return nil
	})

	//logrus.Infof("networks: %v", networks)

	return nil
}

// 创建网络
func CreateNetwork(driver, subnet, name string) error {
	// subnet： 子网网段信息 192.168.0.0/24
	// ParseCIDR 是golang net 包的函数， 功能是将网络的字符串转换成net.IPNet的对象
	_, cidr, _ := net.ParseCIDR(subnet)
	// 通过 IPAM 分配网关 IP ，获取到网段中第一个IP 作为网关的ip
	ip, err := ipAllocator.Allocate(cidr)
	if err != nil {
		return err
	}
	cidr.IP = ip // 网关ip

	//调用指定的网络驱动创建网络，这里的 drivers 字典是各个网络驱动的实例字典，通过调用网络驱动Create 方法创建网络
	nw, err := drivers[driver].Create(cidr.String(), name)
	if err != nil {
		return err
	}

	// 保存网络信息
	return nw.dump(defaultNetworkPath)
}

// 列出网络信息
func ListNetwork() {
	w := tabwriter.NewWriter(os.Stdout, 12, 1, 3, ' ', 0)
	fmt.Fprint(w, "NAME\tIpRange\tDriver\n")
	for _, nw := range networks {
		fmt.Fprintf(w, "%s\t%s\t%s\n",
			nw.Name,
			nw.IpRange.String(),
			nw.Driver,
		)
	}
	if err := w.Flush(); err != nil {
		logrus.Errorf("Flush error %v", err)
		return
	}
}

// 删除网络信息
func DeleteNetwork(networkName string) error {
	nw, ok := networks[networkName]
	if !ok {
		return fmt.Errorf("No Such Network: %s", networkName)
	}
	// 调用 IPAM 的实例 ipAllocator 释放网络网关的 IP
	if err := ipAllocator.Release(nw.IpRange, &nw.IpRange.IP); err != nil {
		return fmt.Errorf("Error Remove Network gateway ip: %s", err)
	}
	// 调用网络驱动删除网络创建的设备与配置
	if err := drivers[nw.Driver].Delete(*nw); err != nil {
		return fmt.Errorf("Error Remove Network DriverError: %s", err)
	}
	// 从网络的配直目录中删除该网络对应的配置文件
	return nw.remove(defaultNetworkPath)
}

//将容器的网络端点加入到容器的网络空间中
//并锁定当前程序所执行的线程，使当前线程进入到容器的网络空间
//返回值是一个函数指针，执行这个返回函数才会退出容器的网络空间，回归到宿主机的网络空间
//这个函数中引用了之前介绍的 github.com/vishvananda/netns 类库来做 Namespace 操作
func enterContainerNetns(enLink *netlink.Link, cinfo *container.ContainerInfo) func() {
	//找到容器的 Net Namespace
	// /proc/[pid}/ns/net 打开这个文件的文件描述符就可以来操作 Net Namespace
	//ContainerInfo 中的 PID，即容器在宿主机上映射的进程 ID
	//它对应 /proc/[pid}/ns/net 就是容器内部的 Net Namespace
	f, err := os.OpenFile(fmt.Sprintf("/proc/%s/ns/net", cinfo.Pid), os.O_RDONLY, 0)
	if err != nil {
		logrus.Errorf("error get container net namespace, %v", err)
	}
	//取到文件的文件描述符
	nsFD := f.Fd()
	//锁定当前程序所执行的线程，如果不锁定操作系统线程的话
	//Go 语言的 goroutine 可能会被调度到别的线程上去
	//就不能保证一直在所需要的网络空间中了
	//所以调用 runtime.LockOSThread 时要先锁定当前程序执行的线程
	runtime.LockOSThread()

	// 修改网络端点veth peer 另外一端,将其移到容器的Net namespace中
	if err = netlink.LinkSetNsFd(*enLink, int(nsFD)); err != nil {
		logrus.Errorf("error set link netns , %v", err)
	}

	// 获取当前的网络namespace
	//通过 netns.Get 方法获得当前网络的 Net Namespace
	//以便后面从容器的 Net Namespace 中退出，回到原本网络的 Net Namespace中
	origns, err := netns.Get()
	if err != nil {
		logrus.Errorf("error get current netns, %v", err)
	}

	// 设置当前进程到新的网络namespace，并在函数执行完成之后再恢复到之前的namespace
	if err = netns.Set(netns.NsHandle(nsFD)); err != nil {
		logrus.Errorf("error set netns, %v", err)
	}
	//返回之前 Net Namespace 的函数
	//在容器的网络空间中，执行完容器配置之后调用此函数就可以将程序恢复到原生的 Net Namespace
	return func() {
		//恢复到上面获取到的之前的 Net Namespace
		netns.Set(origns)
		// 关闭Namespace文件
		origns.Close()
		// 取消对当前程序的线程锁定
		runtime.UnlockOSThread()
		// 关闭Namespace文件
		f.Close()
	}
}

// 进入到容器的网络 Namespace 配置容器网络设备的 IP 地址和路由
//配置容器网络端点的地址和路由
func configEndpointIpAddressAndRoute(ep *Endpoint, cinfo *container.ContainerInfo) error {
	//通过网络端点中"Veth"的
	peerLink, err := netlink.LinkByName(ep.Device.PeerName)
	if err != nil {
		return fmt.Errorf("fail config endpoint: %v", err)
	}
	//将容器的网络端点加入到容器 网络空间中
	//并使这个函数下面的操作都在这个网络空间中进行
	//执行完函数后，恢复为默认的网络空间
	defer enterContainerNetns(&peerLink, cinfo)()

	//获取到容器的 IP 地址及网段, 用于配置容器内部接口地址
	//比如容器 IP 192.168.1.2 ，而网络的网段是 192.168.1.0/24
	//那么这里产出的 IP 字符串就是 192.168.1.2/24 用于容器内 Veth 端点配置
	interfaceIP := *ep.Network.IpRange
	interfaceIP.IP = ep.IPAddress
	//调用 setinterfaceIP 函数设置容器内 Veth 端点的 IP
	if err = setInterfaceIP(ep.Device.PeerName, interfaceIP.String()); err != nil {
		return fmt.Errorf("%v,%s", ep.Network, err)
	}
	//启动容器内的 Veth 端点
	if err = setInterfaceUP(ep.Device.PeerName); err != nil {
		return err
	}

	//net Namespace 中默认本地地址 127.0.0.1的"lo"网卡是关闭状态的
	//启动它以保证容器访问自己的请求
	if err = setInterfaceUP("lo"); err != nil {
		return err
	}

	//设置容器内的外部请求都通过容器内的 Veth 端点访问
	//0.0.0.0/0 的网段，表示所有的 IP 地址段
	_, cidr, _ := net.ParseCIDR("0.0.0.0/0")

	//构建要添加的路由数据，包括网络设备、网关IP 及目的网段
	//相当于 route add -net 0.0.0.0/0 gw {Bridge网桥地址｝ dev ｛容器内的veth 端点设备｝
	defaultRoute := &netlink.Route{
		LinkIndex: peerLink.Attrs().Index,
		Gw:        ep.Network.IpRange.IP,
		Dst:       cidr,
	}
	//调用 netlink RouteAdd 添加路由到容器的网络空间
	//RouteAdd 函数相当于 route add 命令
	if err = netlink.RouteAdd(defaultRoute); err != nil {
		return err
	}

	return nil
}

// 配置容器到宿主机的端口映射
func configPortMapping(ep *Endpoint, cinfo *container.ContainerInfo) error {
	//遍历容器端口映射列表
	for _, pm := range ep.PortMapping {
		//分割成宿主机的端口和容器的端口
		portMapping := strings.Split(pm, ":")
		if len(portMapping) != 2 {
			logrus.Errorf("port mapping format error, %v", pm)
			continue
		}
		//这里采用 exec.Command 的方式直接调用命令配置
		//在 iptables的PREROUTING 中添加 DNAT 规则
		//将宿主机的端口请求转发到容器的地址和端口上
		iptablesCmd := fmt.Sprintf("-t nat -A PREROUTING -p tcp -m tcp --dport %s -j DNAT --to-destination %s:%s",
			portMapping[0], ep.IPAddress.String(), portMapping[1])
		//执行 iptables 命令，添加端口映射转发规则
		cmd := exec.Command("iptables", strings.Split(iptablesCmd, " ")...)
		//err := cmd.Run()
		output, err := cmd.Output()
		if err != nil {
			logrus.Errorf("iptables Output, %v", output)
			continue
		}
	}
	return nil
}

//连接容器到之前创建的网络 mydocker run net testnet -p 8080:80 xxxx
func Connect(networkName string, cinfo *container.ContainerInfo) error {
	// 从 networks 字典中取到容器连接的网络的信息， networks 字典中保存了当前己经创建的网络
	network, ok := networks[networkName]
	if !ok {
		return fmt.Errorf("No Such Network: %s", networkName)
	}

	// 通过调用 IPAM 从网络的网段中获取可用的 IP 作为容器 IP 地址
	ip, err := ipAllocator.Allocate(network.IpRange)
	if err != nil {
		return err
	}

	// 创建网络端点
	ep := &Endpoint{
		ID:          fmt.Sprintf("%s-%s", cinfo.Id, networkName),
		IPAddress:   ip,
		Network:     network,
		PortMapping: cinfo.PortMapping,
	}
	// 调用网络驱动挂载和配置网络端点
	// 传入： network: 网络配置信息  ep： 进程的ip等网络端点信息
	if err = drivers[network.Driver].Connect(network, ep); err != nil {
		return err
	}
	// 进入到容器的网络 Namespace 配置容器网络设备的 IP 地址和路由
	if err = configEndpointIpAddressAndRoute(ep, cinfo); err != nil {
		return err
	}

	// 配置容器到宿主机的端口映射
	return configPortMapping(ep, cinfo)
}

func Disconnect(networkName string, cinfo *container.ContainerInfo) error {
	return nil
}
