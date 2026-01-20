package audio

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

// PCM16BytesToFloat32 将16位PCM小端字节流转换为float32切片（范围-1.0~1.0）
func PCM16BytesToFloat32(pcm []byte) []float32 {
	n := len(pcm) / 2
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		// 取两个字节，按小端序转为int16
		sample := int16(binary.LittleEndian.Uint16(pcm[i*2 : i*2+2]))
		out[i] = float32(sample) / float32(math.MaxInt16)
	}
	return out
}

// float32ToPCMBytes 将 float32 数组转换为 16-bit PCM 字节数组
func Float32ToPCMBytes(samples []float32, pcmBytes []byte) {
	for i, sample := range samples {
		// 将 float32 (-1.0 到 1.0) 转换为 int16 (-32768 到 32767)
		intSample := float32ToInt16(sample)

		// 小端序写入字节数组
		binary.LittleEndian.PutUint16(pcmBytes[i*2:], uint16(intSample))
	}

	return
}

// Float32ToInt16 将float32值转换为int16值（范围-1.0~1.0转换为-32768~32767）
func float32ToInt16(sample float32) int16 {
	if sample > 1.0 {
		return 32767
	} else if sample < -1.0 {
		return -32768
	} else {
		return int16(sample * 32767)
	}
}

// Float32SliceToInt16Slice 将float32切片转换为int16切片
func Float32SliceToInt16Slice(samples []float32) []int16 {
	result := make([]int16, len(samples))
	for i, sample := range samples {
		result[i] = float32ToInt16(sample)
	}
	return result
}

// Float32ToWav 将float32音频样本转换为WAV格式字节数组
func Float32ToWav(samples []float32, sampleRate int, channels int) ([]byte, error) {
	// WAV文件头结构
	var buf bytes.Buffer

	// RIFF头
	buf.WriteString("RIFF")

	// 文件大小（稍后填写）
	fileSizePos := buf.Len()
	binary.Write(&buf, binary.LittleEndian, uint32(0))

	// WAVE标识
	buf.WriteString("WAVE")

	// fmt块
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16)) // fmt块大小
	binary.Write(&buf, binary.LittleEndian, uint16(1))  // PCM格式
	binary.Write(&buf, binary.LittleEndian, uint16(channels))
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*channels*2)) // 字节率
	binary.Write(&buf, binary.LittleEndian, uint16(channels*2))            // 块对齐
	binary.Write(&buf, binary.LittleEndian, uint16(16))                    // 位深度

	// data块
	buf.WriteString("data")
	dataSize := len(samples) * 2
	binary.Write(&buf, binary.LittleEndian, uint32(dataSize))

	// 音频数据
	for _, sample := range samples {
		int16Sample := float32ToInt16(sample)
		binary.Write(&buf, binary.LittleEndian, int16Sample)
	}

	// 回填文件大小
	wavData := buf.Bytes()
	fileSize := uint32(len(wavData) - 8) // 减去RIFF头的8字节
	binary.LittleEndian.PutUint32(wavData[fileSizePos:fileSizePos+4], fileSize)

	return wavData, nil
}

// Float32PCMToOpus 直接将float32 PCM数据转换为Opus帧
func Float32PCMToOpus(samples []float32, inputSampleRate, outputSampleRate, channels, frameDuration int) ([][]byte, error) {
	// 先将float32 PCM转换为WAV格式
	wavData, err := Float32ToWav(samples, inputSampleRate, channels)
	if err != nil {
		return nil, fmt.Errorf("转换PCM为WAV失败: %v", err)
	}

	// 计算合适的比特率，frameDuration参数在这里用不上，使用默认比特率
	// 对于语音合成，64kbps通常是一个合适的比特率
	bitRate := 64000 // 64 kbps

	// 然后将WAV转换为Opus帧
	return WavToOpus(wavData, outputSampleRate, channels, bitRate)
}

// int16SliceToBytes 将int16切片转换为[]byte（小端序）
func Int16SliceToBytes(samples []int16) []byte {
	buf := new(bytes.Buffer)
	for _, s := range samples {
		buf.WriteByte(byte(s))
		buf.WriteByte(byte(s >> 8))
	}
	return buf.Bytes()
}

// BuildWavHeader builds a WAV header for PCM S16LE payloads.
func BuildWavHeader(sampleRate int, channels int, dataLen int) []byte {
	if sampleRate <= 0 {
		sampleRate = 16000
	}
	if channels <= 0 {
		channels = 1
	}
	if dataLen < 0 {
		dataLen = 0
	}

	const bitsPerSample = 16
	const fmtChunkSize = 16
	const headerSize = 44
	byteRate := sampleRate * channels * bitsPerSample / 8
	blockAlign := channels * bitsPerSample / 8
	riffSize := uint32(dataLen + headerSize - 8)

	header := make([]byte, headerSize)
	copy(header[0:4], []byte("RIFF"))
	binary.LittleEndian.PutUint32(header[4:8], riffSize)
	copy(header[8:12], []byte("WAVE"))
	copy(header[12:16], []byte("fmt "))
	binary.LittleEndian.PutUint32(header[16:20], fmtChunkSize)
	binary.LittleEndian.PutUint16(header[20:22], 1) // PCM
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(header[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(header[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(header[34:36], bitsPerSample)
	copy(header[36:40], []byte("data"))
	binary.LittleEndian.PutUint32(header[40:44], uint32(dataLen))
	return header
}

func ResampleLinearFloat32(input []float32, inRate, outRate int) []float32 {
	ratio := float64(outRate) / float64(inRate)
	outLen := int(float64(len(input)) * ratio)
	output := make([]float32, outLen)

	for i := 0; i < outLen; i++ {
		pos := float64(i) / ratio
		index := int(pos)
		if index >= len(input)-1 {
			output[i] = input[len(input)-1]
		} else {
			frac := float32(pos - float64(index))
			output[i] = input[index]*(1-frac) + input[index+1]*frac
		}
	}
	return output
}
