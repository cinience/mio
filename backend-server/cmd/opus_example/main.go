package main

import (
	"fmt"
	"os"

	"github.com/godeps/opus"
)

func main() {
	// 音频参数设置
	channels := 1
	sampleRate := 16000 // 16kHz
	fmt.Printf("通道数: %d, 采样率: %d Hz\n", channels, sampleRate)

	// 创建一个编码器，指定应用类型为VoIP (低延迟语音)
	enc, err := opus.NewEncoder(sampleRate, channels, opus.AppVoIP)
	if err != nil {
		fmt.Printf("创建编码器失败: %v\n", err)
		os.Exit(1)
	}

	// 设置比特率为16kbps
	if err = enc.SetBitrate(16000); err != nil {
		fmt.Printf("设置比特率失败: %v\n", err)
		os.Exit(1)
	}

	// 设置复杂度，0-10之间，越高质量越好但CPU消耗越大
	if err = enc.SetComplexity(5); err != nil {
		fmt.Printf("设置复杂度失败: %v\n", err)
		os.Exit(1)
	}

	// 生成20ms的测试PCM数据 (每帧20ms，16kHz采样率 = 320样本)
	frameSize := 320
	pcm := make([]int16, frameSize*channels)

	// 生成一个简单的正弦波进行测试
	for i := 0; i < frameSize; i++ {
		// 简单的正弦波，频率约为440Hz
		value := int16(10000.0 * float64(i%36) / 36.0)
		pcm[i] = value
	}

	// 用于存储编码后的数据
	data := make([]byte, 1000)

	// 编码PCM数据为Opus
	n, err := enc.Encode(pcm, data)
	if err != nil {
		fmt.Printf("编码失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("编码%d个样本为%d字节的Opus数据，压缩率: %.2f%%\n",
		frameSize*channels, n, float64(n)/float64(frameSize*channels*2)*100)

	// 创建解码器进行解码测试
	dec, err := opus.NewDecoder(sampleRate, channels)
	if err != nil {
		fmt.Printf("创建解码器失败: %v\n", err)
		os.Exit(1)
	}

	// 用于存储解码后的PCM数据
	decodedPCM := make([]int16, frameSize*channels)

	// 解码Opus数据为PCM
	samplesDecoded, err := dec.Decode(data[:n], decodedPCM)
	if err != nil {
		fmt.Printf("解码失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("解码%d字节的Opus数据为%d个样本\n", n, samplesDecoded)

	// 计算原始PCM与解码后PCM的差异
	var sumDiff int64
	for i := 0; i < frameSize; i++ {
		diff := int64(pcm[i]) - int64(decodedPCM[i])
		if diff < 0 {
			diff = -diff
		}
		sumDiff += diff
	}
	avgDiff := float64(sumDiff) / float64(frameSize)

	fmt.Printf("原始PCM与解码PCM的平均差异: %.2f\n", avgDiff)
	fmt.Println("Opus编解码示例完成!")
}
