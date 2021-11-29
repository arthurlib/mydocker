package network

import (
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"net"
	"os"
	"path"
	"strings"
)

const ipamDefaultAllocatorPath = "/var/run/mydocker/network/ipam/subnet.json"

type IPAM struct {
	// 分配文件存放位置
	SubnetAllocatorPath string
	// 网段的位图算法的数组map， key是网段，value是分配的位图数组（记录的是分配的ip信息
	Subnets *map[string]string
}

var ipAllocator = &IPAM{
	SubnetAllocatorPath: ipamDefaultAllocatorPath,
}

//加载网段地址分配信息
func (ipam *IPAM) load() error {
	//通过 OS.Stat 函数检查存储文件状态，如果不存在，则说明之前没有分配，则不需要加载
	if _, err := os.Stat(ipam.SubnetAllocatorPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		} else {
			return err
		}
	}
	subnetConfigFile, err := os.Open(ipam.SubnetAllocatorPath)
	defer subnetConfigFile.Close()
	if err != nil {
		return err
	}
	subnetJson := make([]byte, 2000)
	n, err := subnetConfigFile.Read(subnetJson)
	if err != nil {
		return err
	}

	err = json.Unmarshal(subnetJson[:n], ipam.Subnets)
	if err != nil {
		log.Errorf("Error dump allocation info, %v", err)
		return err
	}
	return nil
}

//存储网段地址分配信息
func (ipam *IPAM) dump() error {
	//检查存储文件所在文件夹是否存在，如果不存在则创建， path.Split 函数能够分隔目录和文件
	ipamConfigFileDir, _ := path.Split(ipam.SubnetAllocatorPath)
	if _, err := os.Stat(ipamConfigFileDir); err != nil {
		if os.IsNotExist(err) {
			os.MkdirAll(ipamConfigFileDir, 0644)
		} else {
			return err
		}
	}
	//打开存储文件 os.O_TRUNC 表示如果存在则消空， os.O_CREATE 表示如果不存在则创建
	subnetConfigFile, err := os.OpenFile(ipam.SubnetAllocatorPath, os.O_TRUNC|os.O_WRONLY|os.O_CREATE, 0644)
	defer subnetConfigFile.Close()
	if err != nil {
		return err
	}

	ipamConfigJson, err := json.Marshal(ipam.Subnets)
	if err != nil {
		return err
	}

	_, err = subnetConfigFile.Write(ipamConfigJson)
	if err != nil {
		return err
	}

	return nil
}

//在网段中分配一个可用的 IP 地址
func (ipam *IPAM) Allocate(subnet *net.IPNet) (ip net.IP, err error) {
	// 存放网段中地址分配信息的数组
	ipam.Subnets = &map[string]string{}

	// 从文件中加载已经分配的网段信息
	err = ipam.load()
	if err != nil {
		log.Errorf("Error dump allocation info, %v", err)
	}

	_, subnet, _ = net.ParseCIDR(subnet.String())

	one, size := subnet.Mask.Size()

	//如果之前没有分配过这个网段，则初始化网段的分配配置
	if _, exist := (*ipam.Subnets)[subnet.String()]; !exist {
		(*ipam.Subnets)[subnet.String()] = strings.Repeat("0", 1<<uint8(size-one))
	}

	//遍历网段的位图数组
	for c := range (*ipam.Subnets)[subnet.String()] {
		//找到数组中为"0"的项和数组序号，即可以分配的 IP
		if (*ipam.Subnets)[subnet.String()][c] == '0' {
			//设置这个为"0"的序号值为"1" 即分配这个 IP
			ipalloc := []byte((*ipam.Subnets)[subnet.String()])
			//Go 的字符串，创建之后就不能修改 所以通过转换成 byte 数组，修改后再转换成字符串赋值
			ipalloc[c] = '1'
			//／这里的 IP 为初始ip，比如对于网段 192.168.0.0/16 ，这里就是 192.168.0.0
			(*ipam.Subnets)[subnet.String()] = string(ipalloc)
			ip = subnet.IP
			for t := uint(4); t > 0; t -= 1 {
				[]byte(ip)[4-t] += uint8(c >> ((t - 1) * 8))
			}
			ip[3] += 1
			break
		}
	}

	ipam.dump()
	return
}

// 地址释放
func (ipam *IPAM) Release(subnet *net.IPNet, ipaddr *net.IP) error {
	ipam.Subnets = &map[string]string{}

	_, subnet, _ = net.ParseCIDR(subnet.String())
	//从文件中加载网段的分配信息
	err := ipam.load()
	if err != nil {
		log.Errorf("Error dump allocation info, %v", err)
	}
	//计算工 地址在网段位图数组中的索引位置
	c := 0
	//将 IP 地址转换成 个字节的表示方式
	releaseIP := ipaddr.To4()
	//由于 IP 是从 开始分配的，所以转换成索引应减
	releaseIP[3] -= 1
	for t := uint(4); t > 0; t -= 1 {
		//与分配 IP 相反，释放 IP 获得索引的方式是 IP 地址的每一位相减之后分别左移将对应的数值加到索引上
		c += int(releaseIP[t-1]-subnet.IP[t-1]) << ((4 - t) * 8)
	}
	//将分配的位图数组中索引位置的值置0
	ipalloc := []byte((*ipam.Subnets)[subnet.String()])
	ipalloc[c] = '0'
	(*ipam.Subnets)[subnet.String()] = string(ipalloc)

	// 保存到文件
	ipam.dump()
	return nil
}
