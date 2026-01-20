package ip_geo

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/xdb"
)

//go:embed ip2region_v4.xdb
var embeddedIPv4DB []byte

// IpGeo 表示一次 IP 定位结果
type IpGeo struct {
	Country  string
	Province string
	City     string
	ISP      string
}

// Searcher 封装 ip2region 查询逻辑
type Searcher struct {
	buffer   []byte
	once     sync.Once
	searcher *xdb.Searcher
	initErr  error
}

// NewSearcher 创建一个使用内置数据库的搜索实例
func NewSearcher() *Searcher {
	return &Searcher{
		buffer: embeddedIPv4DB,
	}
}

// NewSearcherFromFile 使用外部 xdb 文件创建搜索实例
func NewSearcherFromFile(dbPath string) (*Searcher, error) {
	buf, err := xdb.LoadContentFromFile(dbPath)
	if err != nil {
		return nil, fmt.Errorf("load %s failed: %w", dbPath, err)
	}

	return &Searcher{
		buffer: buf,
	}, nil
}

// ensure初始化查询器
func (s *Searcher) ensure() error {
	if len(s.buffer) == 0 {
		return fmt.Errorf("ip geolocation database buffer is empty")
	}

	s.once.Do(func() {
		s.searcher, s.initErr = xdb.NewWithBuffer(xdb.IPv4, s.buffer)
	})
	return s.initErr
}

// Lookup 查询 IP 地址
func (s *Searcher) Lookup(ip string) (*IpGeo, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}

	if strings.Contains(ip, ":") {
		return nil, fmt.Errorf("ipv6 not support")
	}

	result, err := s.searcher.SearchByStr(ip)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(result, "|")
	if len(parts) < 3 {
		return nil, fmt.Errorf("ip geo not found")
	}

	geo := &IpGeo{
		Country:  parts[0],
		Province: parts[1],
		City:     parts[2],
	}
	if len(parts) > 3 {
		geo.ISP = parts[3]
	}

	return geo, nil
}

// Close 释放搜索器资源
func (s *Searcher) Close() {
	if s.searcher != nil {
		s.searcher.Close()
	}
}

var defaultSearcher = NewSearcher()

// IPGeoInit 使用指定文件初始化全局搜索器
func IPGeoInit(dbPath string) error {
	searcher, err := NewSearcherFromFile(dbPath)
	if err != nil {
		return err
	}
	defaultSearcher = searcher
	return nil
}

// IPGeoGet 使用全局搜索器查询
func IPGeoGet(ip string) (*IpGeo, error) {
	return defaultSearcher.Lookup(ip)
}
