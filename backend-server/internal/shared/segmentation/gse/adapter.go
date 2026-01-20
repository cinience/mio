package gse

import (
	gse "github.com/go-ego/gse"
)

// GSE 适配器，实现 Segmenter 接口
type adapter struct {
	seg gse.Segmenter
}

// 创建 GSE 适配器
func newAdapter() *adapter {
	return &adapter{
		seg: gse.Segmenter{},
	}
}

// 实现 Segmenter 接口
func (g *adapter) LoadDict(path ...string) error {
	if len(path) > 0 {
		return g.seg.LoadDict(path[0])
	}
	return g.seg.LoadDict()
}

func (g *adapter) Cut(text string, hmm bool) []string {
	return g.seg.Cut(text, hmm)
}
