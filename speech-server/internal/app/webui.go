package app

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	openairt "github.com/WqyJh/go-openai-realtime/v2"

	"speech-server/internal/logger"
	"speech-server/internal/realtime"
)

const webuiTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Speech Server - Text to Voice</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            display: flex;
            justify-content: center;
            align-items: center;
            padding: 20px;
        }

        .container {
            background: white;
            border-radius: 20px;
            box-shadow: 0 20px 60px rgba(0, 0, 0, 0.3);
            padding: 40px;
            max-width: 600px;
            width: 100%;
        }

        h1 {
            color: #333;
            margin-bottom: 10px;
            font-size: 28px;
            text-align: center;
        }

        .subtitle {
            color: #666;
            text-align: center;
            margin-bottom: 30px;
            font-size: 14px;
        }

        .form-group {
            margin-bottom: 25px;
        }

        label {
            display: block;
            margin-bottom: 8px;
            color: #555;
            font-weight: 600;
            font-size: 14px;
        }

        textarea {
            width: 100%;
            padding: 12px;
            border: 2px solid #e0e0e0;
            border-radius: 10px;
            font-size: 16px;
            font-family: inherit;
            resize: vertical;
            min-height: 120px;
            transition: border-color 0.3s;
        }

        textarea:focus {
            outline: none;
            border-color: #667eea;
        }

        .input-row {
            display: flex;
            gap: 15px;
        }

        .input-row .form-group {
            flex: 1;
        }

        input[type="number"],
        select {
            width: 100%;
            padding: 12px;
            border: 2px solid #e0e0e0;
            border-radius: 10px;
            font-size: 16px;
            font-family: inherit;
            transition: border-color 0.3s;
        }

        input[type="number"]:focus,
        select:focus {
            outline: none;
            border-color: #667eea;
        }

        button {
            width: 100%;
            padding: 15px;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            border: none;
            border-radius: 10px;
            font-size: 18px;
            font-weight: 600;
            cursor: pointer;
            transition: transform 0.2s, box-shadow 0.2s;
        }

        button:hover:not(:disabled) {
            transform: translateY(-2px);
            box-shadow: 0 10px 20px rgba(102, 126, 234, 0.4);
        }

        button:active:not(:disabled) {
            transform: translateY(0);
        }

        button:disabled {
            opacity: 0.6;
            cursor: not-allowed;
        }

        .status {
            margin-top: 20px;
            padding: 12px;
            border-radius: 10px;
            text-align: center;
            font-weight: 500;
            display: none;
        }

        .status.info {
            background: #e3f2fd;
            color: #1976d2;
            display: block;
        }

        .status.success {
            background: #e8f5e9;
            color: #388e3c;
            display: block;
        }

        .status.error {
            background: #ffebee;
            color: #d32f2f;
            display: block;
        }

        .audio-player {
            margin-top: 20px;
            display: none;
        }

        audio {
            width: 100%;
            border-radius: 10px;
        }

        .note {
            margin-top: 30px;
            padding: 15px;
            background: #f5f5f5;
            border-radius: 10px;
            font-size: 13px;
            color: #666;
            line-height: 1.6;
        }

        .note strong {
            color: #333;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>Text to Voice</h1>
        <p class="subtitle">输入文本生成语音</p>

        <form id="ttsForm">
            <div class="form-group">
                <label for="text">文本内容</label>
                <textarea id="text" name="text" placeholder="请输入要转换的文本..." required>你好，这是一个语音合成测试，今天是2024年，温度大约23度。你知道1+1=多少吗？有人说某些情况下1+1>2，你觉得呢？Now let's try some English sentences. Speech synthesis should handle multilingual input smoothly.</textarea>
            </div>

            <div class="input-row">
                <div class="form-group">
                    <label for="voice">发音人</label>
                    <select id="voice" name="voice">
{{VOICE_OPTIONS}}
                    </select>
                </div>

                <div class="form-group">
                    <label for="speed">速度</label>
                    <input type="number" id="speed" name="speed" min="0.5" max="2.0" step="0.1" value="1.0">
                </div>

                <div class="form-group">
                    <label for="sampleRate">采样率</label>
                    <select id="sampleRate" name="sampleRate">
                        <option value="8000">8 kHz</option>
                        <option value="16000" selected>16 kHz</option>
                        <option value="22050">22.05 kHz</option>
                        <option value="24000">24 kHz</option>
                    </select>
                </div>
            </div>

            <button type="submit" id="generateBtn">生成语音</button>
        </form>

        <div id="status" class="status"></div>

        <div id="audioPlayer" class="audio-player">
            <audio id="audio" controls></audio>
        </div>

        <div class="note">
            <strong>说明：</strong><br>
            • 语音选项：选择不同的语音风格<br>
            • 速度范围：0.5 (慢) 到 2.0 (快)，默认 1.0<br>
            • 采样率：默认 16 kHz，可根据需求切换常见采样率<br>
            • 生成的音频将自动播放
        </div>
    </div>

    <script>
        const form = document.getElementById('ttsForm');
        const generateBtn = document.getElementById('generateBtn');
        const statusDiv = document.getElementById('status');
        const audioPlayer = document.getElementById('audioPlayer');
        const audio = document.getElementById('audio');

        function showStatus(message, type) {
            statusDiv.textContent = message;
            statusDiv.className = 'status ' + type;
        }

        function hideStatus() {
            statusDiv.className = 'status';
        }

        form.addEventListener('submit', async (e) => {
            e.preventDefault();

            const text = document.getElementById('text').value.trim();
            const voice = document.getElementById('voice').value;
            const speed = parseFloat(document.getElementById('speed').value);
            const sampleRate = parseInt(document.getElementById('sampleRate').value, 10);

            if (!text) {
                showStatus('请输入文本内容', 'error');
                return;
            }

            generateBtn.disabled = true;
            showStatus('正在生成语音...', 'info');
            audioPlayer.style.display = 'none';

            try {
                const response = await fetch('/api/tts', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                    },
                    body: JSON.stringify({
                        text: text,
                        voice: voice,
                        speed: speed,
                        sample_rate: sampleRate
                    })
                });

                if (!response.ok) {
                    const error = await response.json();
                    throw new Error(error.error || '生成失败');
                }

                const result = await response.json();

                // Convert base64 to blob
                const audioData = atob(result.audio);
                const arrayBuffer = new ArrayBuffer(audioData.length);
                const view = new Uint8Array(arrayBuffer);
                for (let i = 0; i < audioData.length; i++) {
                    view[i] = audioData.charCodeAt(i);
                }

                const blob = new Blob([arrayBuffer], { type: 'audio/wav' });
                const url = URL.createObjectURL(blob);

                audio.src = url;
                audioPlayer.style.display = 'block';
                audio.play();

                showStatus('语音生成成功！', 'success');
            } catch (error) {
                showStatus('生成失败: ' + error.message, 'error');
            } finally {
                generateBtn.disabled = false;
            }
        });
    </script>
</body>
</html>`

func (a *Application) registerWebUIRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/", a.handleWebUI)
	mux.HandleFunc("/api/tts", a.handleTTSAPI)
}

func (a *Application) handleWebUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	options := buildVoiceOptions(a.voiceMap, a.defaultVoice)
	html := strings.Replace(webuiTemplate, "{{VOICE_OPTIONS}}", options, 1)
	if _, err := w.Write([]byte(html)); err != nil {
		logger.Errorf("web ui write error: %v", err)
	}
}

type ttsRequest struct {
	Text       string  `json:"text"`
	Voice      string  `json:"voice"`
	Speed      float32 `json:"speed"`
	SampleRate int     `json:"sample_rate"`
}

type ttsResponse struct {
	Audio      string `json:"audio"`
	SampleRate int    `json:"sample_rate"`
	Text       string `json:"text"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func buildVoiceOptions(voiceMap map[openairt.Voice]int, defaultVoice openairt.Voice) string {
	if len(voiceMap) == 0 {
		voiceMap = map[openairt.Voice]int{
			realtime.NormalizeVoice(openairt.VoiceAlloy): 0,
		}
	}

	voices := make([]string, 0, len(voiceMap))
	for voice := range voiceMap {
		trimmed := strings.TrimSpace(string(voice))
		if trimmed == "" {
			continue
		}
		voices = append(voices, trimmed)
	}
	if len(voices) == 0 {
		voices = append(voices, string(openairt.VoiceAlloy))
	}
	sort.Strings(voices)

	normDefault := realtime.NormalizeVoice(defaultVoice)
	if normDefault == "" {
		normDefault = realtime.NormalizeVoice(openairt.Voice(voices[0]))
	}

	var sb strings.Builder
	for _, v := range voices {
		value := strings.TrimSpace(v)
		if value == "" {
			continue
		}
		voiceID := realtime.NormalizeVoice(openairt.Voice(value))
		selected := ""
		if voiceID == normDefault {
			selected = " selected"
		}
		sb.WriteString("                        <option value=\"")
		sb.WriteString(value)
		sb.WriteString("\"")
		sb.WriteString(selected)
		sb.WriteString(">")
		sb.WriteString(value)
		sb.WriteString("</option>\n")
	}
	return sb.String()
}

func (a *Application) handleTTSAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		if err := json.NewEncoder(w).Encode(errorResponse{Error: "Method not allowed"}); err != nil {
			logger.Errorf("error response encode failed: %v", err)
		}
		return
	}

	if a.tts == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(errorResponse{Error: "TTS service not configured"}); err != nil {
			logger.Errorf("error response encode failed: %v", err)
		}
		return
	}

	var req ttsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(errorResponse{Error: "Invalid request: " + err.Error()}); err != nil {
			logger.Errorf("error response encode failed: %v", err)
		}
		return
	}

	if req.Text == "" {
		w.WriteHeader(http.StatusBadRequest)
		if err := json.NewEncoder(w).Encode(errorResponse{Error: "Text is required"}); err != nil {
			logger.Errorf("error response encode failed: %v", err)
		}
		return
	}

	if req.Speed <= 0 {
		req.Speed = 1.0
	}

	// Map voice to speaker ID
	speakerID := 0
	if len(a.voiceMap) > 0 {
		voice := realtime.NormalizeVoice(openairt.Voice(req.Voice))
		if id, ok := a.voiceMap[voice]; ok {
			speakerID = id
		}
	}

	logger.Infof("TTS API request: text=%q voice=%s speed=%.2f speaker=%d", req.Text, req.Voice, req.Speed, speakerID)

	ttsInput := req.Text
	if a.normalizer != nil {
		normalized := a.normalizer.Normalize(ttsInput)
		if normalized != ttsInput {
			logger.Debugf("TTS API normalized text: %q -> %q", ttsInput, normalized)
		}
		ttsInput = normalized
	}

	samples, sampleRate, err := a.tts.Generate(ttsInput, speakerID, req.Speed)
	if err != nil {
		logger.Errorf("TTS generation error: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(errorResponse{Error: "TTS generation failed: " + err.Error()}); err != nil {
			logger.Errorf("error response encode failed: %v", err)
		}
		return
	}

	if len(samples) == 0 {
		w.WriteHeader(http.StatusInternalServerError)
		if err := json.NewEncoder(w).Encode(errorResponse{Error: "TTS generated empty audio"}); err != nil {
			logger.Errorf("error response encode failed: %v", err)
		}
		return
	}

	targetRate := req.SampleRate
	if targetRate <= 0 {
		targetRate = 16000
	}

	outputSamples := samples
	outputRate := sampleRate
	if targetRate > 0 && targetRate != sampleRate {
		outputSamples = realtime.ResamplePCM(samples, sampleRate, targetRate)
		outputRate = targetRate
	}

	// Convert float32 samples to PCM16
	pcmData := realtime.Float32ToPCM16(outputSamples)

	// Create WAV file
	wavData := createWAVFile(pcmData, outputRate)

	// Encode to base64
	audioBase64 := base64.StdEncoding.EncodeToString(wavData)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(ttsResponse{
		Audio:      audioBase64,
		SampleRate: outputRate,
		Text:       req.Text,
	}); err != nil {
		logger.Errorf("tts response encode failed: %v", err)
	}

	logger.Infof("TTS API response: samples=%d rate=%dHz->%dHz size=%dB", len(samples), sampleRate, outputRate, len(wavData))
}

// createWAVFile creates a WAV file header and combines it with PCM data
func createWAVFile(pcmData []byte, sampleRate int) []byte {
	numChannels := 1
	bitsPerSample := 16
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8
	dataSize := len(pcmData)
	fileSize := 36 + dataSize

	header := make([]byte, 44)

	// RIFF header
	copy(header[0:4], []byte("RIFF"))
	writeUint32(header[4:8], uint32(fileSize))
	copy(header[8:12], []byte("WAVE"))

	// fmt chunk
	copy(header[12:16], []byte("fmt "))
	writeUint32(header[16:20], 16) // chunk size
	writeUint16(header[20:22], 1)  // audio format (PCM)
	writeUint16(header[22:24], uint16(numChannels))
	writeUint32(header[24:28], uint32(sampleRate))
	writeUint32(header[28:32], uint32(byteRate))
	writeUint16(header[32:34], uint16(blockAlign))
	writeUint16(header[34:36], uint16(bitsPerSample))

	// data chunk
	copy(header[36:40], []byte("data"))
	writeUint32(header[40:44], uint32(dataSize))

	// Combine header and data
	result := make([]byte, 0, len(header)+len(pcmData))
	result = append(result, header...)
	result = append(result, pcmData...)

	return result
}

func writeUint32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func writeUint16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}
