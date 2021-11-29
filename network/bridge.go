package network

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"net"
	"os/exec"
	"strings"
	"time"
)

type BridgeNetworkDriver struct {
}

func (d *BridgeNetworkDriver) Name() string {
	return "bridge"
}

func (d *BridgeNetworkDriver) Create(subnet string, name string) (*Network, error) {
	// subnet: 子网网段
	// name: 子网命名
	ip, ipRange, _ := net.ParseCIDR(subnet)
	ipRange.IP = ip
	n := &Network{
		Name:    name,     // 网络名
		IpRange: ipRange,  // 网段
		Driver:  d.Name(), // 网络驱动
	}
	// 初始化网桥
	err := d.initBridge(n)
	if err != nil {
		log.Errorf("error init bridge: %v", err)
	}

	return n, err
}

func (d *BridgeNetworkDriver) Delete(network Network) error {
	//网络名即 Linux Bridge 的设备名
	bridgeName := network.Name
	//通过 netlink 库的 LinkByName 找到网络对应的设备
	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return err
	}
	//删除网络对应的 Linux Bridge 设备
	return netlink.LinkDel(br)
}

//连接一个网络和网络端点
func (d *BridgeNetworkDriver) Connect(network *Network, endpoint *Endpoint) error {
	//网络名即 Linux Bridge 的设备名
	bridgeName := network.Name
	//通过接口名获取到 Linux Bridge 接口的对象和接口属性
	br, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return err
	}
	//创建 Veth 接口的配置
	la := netlink.NewLinkAttrs()
	//／由于 linux 接口名的限制，名字取 endpoint ID 的前5位
	la.Name = endpoint.ID[:5]
	//通过设置 Veth 接口 master 属性，设置这个 Veth 的一端挂载到网络对应的 Linux Bridge上
	la.MasterIndex = br.Attrs().Index

	//创建 Veth 对象，通过 PeerName 配置 Veth 另外一端的接口名
	//配置 Veth 另外一端的名字 cif-{endpoint ID 的前5位｝
	endpoint.Device = netlink.Veth{
		LinkAttrs: la,
		PeerName:  "cif-" + endpoint.ID[:5],
	}

	//调用 netlink的LinkAdd 方法创建出这个 Veth 接口
	//因为上面指定了 link的MasterIndex 是网络对应的 Linux Bridge
	//所以 Veth 的一端就己经挂载到了网络对应的 Linux Bridge上
	if err = netlink.LinkAdd(&endpoint.Device); err != nil {
		return fmt.Errorf("Error Add Endpoint Device: %v", err)
	}

	//调用 netlink的LinkSetUp 方法，设置 Veth 启动
	//相当于 ip link set xxx up 命令
	if err = netlink.LinkSetUp(&endpoint.Device); err != nil {
		return fmt.Errorf("Error Add Endpoint Device: %v", err)
	}
	return nil
}

func (d *BridgeNetworkDriver) Disconnect(network Network, endpoint *Endpoint) error {
	return nil
}

// 初始化网桥  创边Bridge虚拟设备->设置Bridge设备地址和路由->启动Bridge设备->设置iptables SNAT 规则
func (d *BridgeNetworkDriver) initBridge(n *Network) error {
	// try to get bridge by name, if it already exists then just exit
	//  1.创建 Bridge 虚拟设备
	bridgeName := n.Name
	if err := createBridgeInterface(bridgeName); err != nil {
		return fmt.Errorf("Error add bridge： %s, Error: %v", bridgeName, err)
	}

	// Set bridge IP
	//2. 设置 Bridge 设备的地址和路由
	gatewayIP := *n.IpRange
	gatewayIP.IP = n.IpRange.IP

	if err := setInterfaceIP(bridgeName, gatewayIP.String()); err != nil {
		return fmt.Errorf("Error assigning address: %s on bridge: %s with an error of: %v", gatewayIP, bridgeName, err)
	}

	//3. 启动 Bridge 设备
	if err := setInterfaceUP(bridgeName); err != nil {
		return fmt.Errorf("Error set bridge up: %s, Error: %v", bridgeName, err)
	}

	// Setup iptables
	//4.设置 iptabels SNAT 规则
	if err := setupIPTables(bridgeName, n.IpRange); err != nil {
		return fmt.Errorf("Error setting iptables for %s: %v", bridgeName, err)
	}

	return nil
}

// deleteBridge deletes the bridge
func (d *BridgeNetworkDriver) deleteBridge(n *Network) error {
	bridgeName := n.Name

	// get the link
	l, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return fmt.Errorf("Getting link with name %s failed: %v", bridgeName, err)
	}

	// delete the link
	if err := netlink.LinkDel(l); err != nil {
		return fmt.Errorf("Failed to remove bridge interface %s delete: %v", bridgeName, err)
	}

	return nil
}

//  1.创建 Bridge 虚拟设备
func createBridgeInterface(bridgeName string) error {
	//先检查是否己经存在了这个同名的 Bridge 设备
	_, err := net.InterfaceByName(bridgeName)
	//如果已经存在或者报错则返回创建错
	if err == nil || !strings.Contains(err.Error(), "no such network interface") {
		return err
	}

	// create *netlink.Bridge object
	//初始化 netlink的Link 对象 Link 的名字即 Bridge 虚拟设备的名字
	la := netlink.NewLinkAttrs()
	la.Name = bridgeName

	//／使用刚才创建的 Link 的属性创建 netlink Bridge 对象
	br := &netlink.Bridge{LinkAttrs: la}
	//调用 netlink的Linkadd 方法，创建Bridge 虚拟网络设备
	//netlink的Linkadd 方法是用来创建虚拟网络设备的，相当于 ip link add xxxx
	if err := netlink.LinkAdd(br); err != nil {
		return fmt.Errorf("Bridge creation failed for bridge %s: %v", bridgeName, err)
	}
	return nil
}

// 启动 Bridge 设备
func setInterfaceUP(interfaceName string) error {
	iface, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return fmt.Errorf("Error retrieving a link named [ %s ]: %v", iface.Attrs().Name, err)
	}

	//通过"netlink"的"LinkSetUp" 方法设直接口状态为"UP"状态
	//等价于 ip link set xxx up 命令
	if err := netlink.LinkSetUp(iface); err != nil {
		return fmt.Errorf("Error enabling interface for %s: %v", interfaceName, err)
	}
	//linux 的网络设备只有设置成 UP 态后才能处理和转发请求
	return nil
}

// Set the IP addr of a netlink interface
// 设置 Bridge 设备的地址和路由
// 设置一个网络接口的 IP 地址，例如 setinterfaceIP("testbridge", "192.168.0.1/24")
func setInterfaceIP(name string, rawIP string) error {
	retries := 2
	var iface netlink.Link
	var err error
	for i := 0; i < retries; i++ {
		//通过 netlink的LinkByName 方法找到需要设置的网络接口
		iface, err = netlink.LinkByName(name)
		if err == nil {
			break
		}
		log.Debugf("error retrieving new bridge netlink link [ %s ]... retrying", name)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return fmt.Errorf("Abandoning retrieving the new bridge link from netlink, Run [ ip link ] to troubleshoot the error: %v", err)
	}
	//由于 netlink.ParseIPNet 是对 net.ParseCIDR的一个封装，因此可以将 net.ParseCIDR的返回值中的 IP和net 整合。
	//返回值中的 ipNet 既包含了网段的信息， 192 168.0.0/24 ，也包含了原始的ip 192.168.0.1
	ipNet, err := netlink.ParseIPNet(rawIP)
	if err != nil {
		return err
	}
	// 通过 netlink.AddrAdd 给网络接口配置地址，相当于ip addr add xxx 的命令
	// 同时如果配置了地址所在网段的信息，例如 192.168.0 0/24
	// 还会配置路由表 192.168.0.0/24 转发到这个 testbridge 的网络接口上
	addr := &netlink.Addr{IPNet: ipNet, Peer: ipNet, Label: "", Flags: 0, Scope: 0}
	//通过调用 netlink AddrAdd 方法，配置 Linux Bridge 的地址和路由表。
	return netlink.AddrAdd(iface, addr)
}

//设置 iptabels SNAT 规则
func setupIPTables(bridgeName string, subnet *net.IPNet) error {
	//iptables -t nat -A POSTROUTING -s <bridgeName> ! -o <bridgeName> -j MASQUERADE
	iptablesCmd := fmt.Sprintf("-t nat -A POSTROUTING -s %s ! -o %s -j MASQUERADE", subnet.String(), bridgeName)
	cmd := exec.Command("iptables", strings.Split(iptablesCmd, " ")...)
	//err := cmd.Run()
	output, err := cmd.Output()
	if err != nil {
		log.Errorf("iptables Output, %v", output)
	}
	return err
}
