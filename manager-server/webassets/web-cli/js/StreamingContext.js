import { log } from './utils/logger.js';

// 音频流播放上下文类 - 简化版本，参考 xiaozhi-web-client 的成功实现
export class StreamingContext {
    constructor(opusDecoder, audioContext, sampleRate, channels, minAudioDuration) {
        this.opusDecoder = opusDecoder;
        this.audioContext = audioContext;

        // 音频参数
        this.sampleRate = sampleRate;
        this.channels = channels;
        this.minAudioDuration = minAudioDuration;

        // 使用简单的数组队列，避免复杂的多层队列导致数据丢失
        this.opusQueue = [];      // 待解码的Opus帧队列
        this.pcmQueue = [];       // 已解码的PCM样本队列
        this.isPlaying = false;   // 是否正在播放
        this.nextPlayTime = 0;    // 下一个音频片段的播放时间
        this.playbackActive = false; // 播放循环是否激活
        
        // 统计信息
        this.totalFramesReceived = 0;
        this.totalFramesDecoded = 0;
        this.totalSamplesPlayed = 0;
        
        // 回调函数
        this.onPlaybackFinished = null; // 播放完成时的回调
        
        // TTS 流状态
        this.ttsStreamActive = false; // 是否有活跃的 TTS 流（从 start 到 stop）
        this.waitRetryCount = 0; // 等待重试次数
        this.maxWaitRetries = 10; // 最大等待重试次数（10次 * 300ms = 3秒）
    }

    // 添加Opus帧到队列
    pushAudioBuffer(opusFrames) {
        if (!Array.isArray(opusFrames)) {
            opusFrames = [opusFrames];
        }
        
        this.opusQueue.push(...opusFrames);
        this.totalFramesReceived += opusFrames.length;
        
        // 每接收 50 帧输出一次统计日志（降低日志频率）
        if (this.totalFramesReceived % 50 === 0) {
            log(`📊 音频接收统计 [总接收: ${this.totalFramesReceived}帧, Opus队列: ${this.opusQueue.length}帧, PCM队列: ${this.pcmQueue.length}样本, TTS流: ${this.ttsStreamActive ? '🟢活跃' : '⚪未激活'}]`, 'debug');
        } else {
            log(`接收到 ${opusFrames.length} 个Opus帧，队列中共 ${this.opusQueue.length} 帧`, 'debug');
        }
        
        // 如果还没开始解码，立即触发解码
        if (!this.decodingActive) {
            this.decodeNextBatch();
        }
    }

    // 将Int16音频数据转换为Float32音频数据
    convertInt16ToFloat32(int16Data) {
        const float32Data = new Float32Array(int16Data.length);
        for (let i = 0; i < int16Data.length; i++) {
            // 将[-32768,32767]范围转换为[-1,1]
            float32Data[i] = int16Data[i] / (int16Data[i] < 0 ? 0x8000 : 0x7FFF);
        }
        return float32Data;
    }

    // 批量解码Opus帧
    decodeNextBatch() {
        if (this.decodingActive || this.opusQueue.length === 0) {
            return;
        }

        this.decodingActive = true;

        // 一次处理所有待解码的帧
        const framesToDecode = [...this.opusQueue];
        this.opusQueue = [];

        let decodedCount = 0;
        for (const frame of framesToDecode) {
            try {
                const pcmData = this.opusDecoder.decode(frame);
                if (pcmData && pcmData.length > 0) {
                    const float32Data = this.convertInt16ToFloat32(pcmData);
                    // 直接添加到PCM队列
                    this.pcmQueue.push(...float32Data);
                    decodedCount++;
                }
            } catch (error) {
                log(`Opus解码失败: ${error.message}`, 'error');
            }
        }

        this.totalFramesDecoded += decodedCount;
        this.decodingActive = false;

        if (decodedCount > 0) {
            log(`成功解码 ${decodedCount}/${framesToDecode.length} 帧，PCM队列长度: ${this.pcmQueue.length} 样本`, 'debug');
            
            // 如果播放未激活但有足够数据，启动播放
            if (!this.isPlaying && this.pcmQueue.length >= this.sampleRate * this.minAudioDuration) {
                this.startPlayback();
            }
        }

        // 如果还有待解码的帧，继续解码
        if (this.opusQueue.length > 0) {
            setTimeout(() => this.decodeNextBatch(), 0);
        }
    }

    // 启动播放
    startPlayback() {
        if (this.isPlaying) {
            return;
        }

        this.isPlaying = true;
        this.playbackActive = true;
        this.waitRetryCount = 0; // 重置等待计数器
        
        // 初始化播放时间
        if (this.nextPlayTime === 0) {
            this.nextPlayTime = this.audioContext.currentTime;
        }

        log(`✅ 开始音频播放 [TTS流: ${this.ttsStreamActive ? '活跃🟢' : '未激活⚪'}, Opus队列: ${this.opusQueue.length}帧, PCM队列: ${this.pcmQueue.length}样本, 等待计数: ${this.waitRetryCount}]`, 'info');
        this.playNextChunk();
    }

    // 播放下一个音频块
    playNextChunk() {
        if (!this.playbackActive) {
            return;
        }

        // 检查是否有足够的数据播放
        if (this.pcmQueue.length === 0) {
            // 没有数据了，检查是否还有待解码的帧
            if (this.opusQueue.length > 0) {
                log('PCM队列为空，等待解码...', 'debug');
                this.decodeNextBatch();
                // 100ms后重试
                setTimeout(() => this.playNextChunk(), 100);
                return;
            }
            
            // 没有数据也没有待解码的帧，但可能还有新的数据正在传输中
            // 智能等待机制：
            // - 如果 TTS 流仍然活跃（还没收到 stop），持续等待新数据
            // - 如果 TTS 流已结束但重试次数未超限，继续等待
            // - 否则停止播放
            
            if (this.ttsStreamActive) {
                // TTS 流还活跃，说明可能还有新的 sentence 正在生成
                log(`⏸️ 队列为空但TTS流活跃🟢，等待新sentence [重试 ${this.waitRetryCount + 1}/${this.maxWaitRetries}, 已播放: ${(this.totalSamplesPlayed / this.sampleRate).toFixed(2)}s]`, 'debug');
            } else {
                // TTS 流已结束，但可能还有数据在传输
                log(`⏸️ 队列为空且TTS流已结束⚪，等待剩余数据 [重试 ${this.waitRetryCount + 1}, 已播放: ${(this.totalSamplesPlayed / this.sampleRate).toFixed(2)}s]`, 'debug');
            }
            
            this.waitRetryCount++;
            
            setTimeout(() => {
                // 再次检查队列
                if (!this.playbackActive) {
                    return; // 已经被外部停止
                }
                
                   if (this.pcmQueue.length > 0 || this.opusQueue.length > 0) {
                       // 检测到新数据，重置重试计数器并继续播放
                       log(`🔄 检测到新数据 [Opus: ${this.opusQueue.length}帧, PCM: ${this.pcmQueue.length}样本]，重置等待计数器并继续播放`, 'debug');
                       this.waitRetryCount = 0;
                       this.playNextChunk();
                   } else if (this.ttsStreamActive) {
                       // TTS 流仍活跃，必须继续等待，不限制重试次数
                       // 只有收到 tts stop 才能真正结束
                       if (this.waitRetryCount < this.maxWaitRetries) {
                           log(`⏳ TTS流活跃🟢，继续等待新sentence [重试 ${this.waitRetryCount + 1}/${this.maxWaitRetries}, 已等待 ${(this.waitRetryCount + 1) * 0.3}s, TTS状态: ${this.ttsStreamActive ? '🟢' : '⚪'}]`, 'info');
                           this.playNextChunk();
                       } else {
                           // 即使超时也不能停止，继续等待但记录警告
                           log(`⚠️ TTS流活跃🟢但等待时间过长 [已重试 ${this.waitRetryCount}次, 约${(this.waitRetryCount * 0.3).toFixed(1)}s, TTS状态: ${this.ttsStreamActive ? '🟢' : '⚪'}]，继续等待...`, 'warning');
                           this.playNextChunk();
                       }
                   } else if (this.waitRetryCount < 3) {
                       // TTS 流已结束，只等待额外的 3 次（900ms）给数据传输
                       log(`⏱️ TTS流已结束⚪，等待剩余数据 [重试 ${this.waitRetryCount + 1}/3, 已等待 ${(this.waitRetryCount + 1) * 0.3}s, TTS状态: ${this.ttsStreamActive ? '🟢' : '⚪'}]`, 'info');
                       this.playNextChunk();
                   } else {
                       // TTS 流已结束且等待时间足够，真正结束播放
                       log(`✅ 播放完成 [接收 ${this.totalFramesReceived}帧, 解码 ${this.totalFramesDecoded}帧, 播放 ${this.totalSamplesPlayed}样本, 总时长 ${(this.totalSamplesPlayed / this.sampleRate).toFixed(2)}s]`, 'info');
                       log(`💡 停止原因：TTS流状态=${this.ttsStreamActive ? '🟢活跃(不应该停止!)' : '⚪已结束'}, 等待次数=${this.waitRetryCount}, Opus队列=${this.opusQueue.length}, PCM队列=${this.pcmQueue.length}`, 'warning');
                       this.waitRetryCount = 0;
                       this.stopPlayback();
                   }
            }, 300);
            return;
        }

        try {
            // 每次播放0.5-1秒的数据，避免缓冲区过大或过小
            const samplesToPlay = Math.min(this.pcmQueue.length, this.sampleRate);
            const samples = this.pcmQueue.splice(0, samplesToPlay);

            // 创建音频缓冲区
            const audioBuffer = this.audioContext.createBuffer(
                this.channels,
                samples.length,
                this.sampleRate
            );
            audioBuffer.copyToChannel(new Float32Array(samples), 0);

            // 创建音频源
            const source = this.audioContext.createBufferSource();
            source.buffer = audioBuffer;

            // 创建增益节点用于淡入淡出
            const gainNode = this.audioContext.createGain();
            source.connect(gainNode);
            gainNode.connect(this.audioContext.destination);

            // 计算播放时间，确保无缝连接
            const currentTime = this.audioContext.currentTime;
            const startTime = Math.max(currentTime, this.nextPlayTime);
            const duration = audioBuffer.duration;

            // 添加淡入淡出效果，避免爆音
            const fadeDuration = Math.min(0.005, duration / 4);
            gainNode.gain.setValueAtTime(0, startTime);
            gainNode.gain.linearRampToValueAtTime(1, startTime + fadeDuration);
            gainNode.gain.setValueAtTime(1, startTime + duration - fadeDuration);
            gainNode.gain.linearRampToValueAtTime(0, startTime + duration);

            // 播放完成后的回调
            source.onended = () => {
                source.disconnect();
                gainNode.disconnect();
                this.totalSamplesPlayed += samples.length;
                
                // 继续播放下一块
                if (this.playbackActive) {
                    // 立即触发下一次播放，不要等待
                    this.playNextChunk();
                }
            };

            // 开始播放
            source.start(startTime);
            this.nextPlayTime = startTime + duration;

            log(`播放 ${samples.length} 样本 (${duration.toFixed(2)}秒)，剩余 ${this.pcmQueue.length} 样本`, 'debug');

        } catch (error) {
            log(`音频播放错误: ${error.message}`, 'error');
            // 出错后稍等片刻继续尝试
            if (this.playbackActive && (this.pcmQueue.length > 0 || this.opusQueue.length > 0)) {
                setTimeout(() => this.playNextChunk(), 100);
            } else {
                this.stopPlayback();
            }
        }
    }

    // 停止播放
    stopPlayback() {
        // 关键日志：检查停止时的状态
        const hasRemainingData = this.opusQueue.length > 0 || this.pcmQueue.length > 0;
        if (hasRemainingData) {
            log(`⚠️ 停止播放时仍有剩余数据 [Opus: ${this.opusQueue.length}帧, PCM: ${this.pcmQueue.length}样本] - 可能音频未播完！`, 'warning');
        }
        
        this.isPlaying = false;
        this.playbackActive = false;
        this.nextPlayTime = 0;
        this.waitRetryCount = 0; // 重置等待计数器
        this.ttsStreamActive = false; // 重置 TTS 流状态
        log(`🛑 音频播放已停止 [队列状态: Opus ${this.opusQueue.length}帧 + PCM ${this.pcmQueue.length}样本, 状态已重置]`, 'info');
        
        // 通知外部播放已完成
        if (typeof this.onPlaybackFinished === 'function') {
            try {
                log('📢 触发播放完成回调 → 自动测试可以发送下一条消息了', 'info');
                this.onPlaybackFinished();
            } catch (error) {
                log(`❌ 播放完成回调执行出错: ${error.message}`, 'error');
            }
        }
    }

    // 设置 TTS 流状态
    setTtsStreamActive(active) {
        const previousState = this.ttsStreamActive;
        this.ttsStreamActive = active;
        if (active) {
            // TTS 流激活时，重置等待计数器，开始新的等待周期
            this.waitRetryCount = 0;
            log('🟢 TTS流激活 [状态: ⚪→🟢] 重置等待计数器，将无限等待新的sentence直到收到stop', 'info');
        } else {
            // 关键日志：显示收到 stop 时的队列状态
            const remainingAudioTime = (this.pcmQueue.length / this.sampleRate).toFixed(2);
            log(`⚪ TTS流结束 [状态: 🟢→⚪] 剩余音频: Opus ${this.opusQueue.length}帧 + PCM ${this.pcmQueue.length}样本 (约${remainingAudioTime}s), 播放状态: ${this.playbackActive ? '活跃' : '未激活'}, 将在音频播完后900ms内停止`, 'info');
        }
    }
    
    // 重置状态
    // @param silent - 如果为true，不触发播放完成回调（用于开始新的播放流）
    reset(silent = false) {
        // 保存播放状态，判断是否需要触发完成回调
        const wasPlaying = this.isPlaying || this.opusQueue.length > 0 || this.pcmQueue.length > 0;
        const opusCount = this.opusQueue.length;
        const pcmCount = this.pcmQueue.length;
        const wasActive = this.playbackActive;
        
        this.opusQueue = [];
        this.pcmQueue = [];
        this.isPlaying = false;
        this.playbackActive = false;
        this.nextPlayTime = 0;
        this.totalFramesReceived = 0;
        this.totalFramesDecoded = 0;
        this.totalSamplesPlayed = 0;
        this.decodingActive = false;
        this.ttsStreamActive = false;
        this.waitRetryCount = 0;
        
        if (wasPlaying) {
            log(`🔄 音频上下文已重置 [清理前状态: Opus ${opusCount}帧 + PCM ${pcmCount}样本, 播放活跃: ${wasActive}, silent模式: ${silent}]`, 'info');
        } else {
            log(`🔄 音频上下文已重置 [之前无活跃播放]`, 'info');
        }
        
        // 如果之前正在播放或有待播放的数据，触发播放完成回调
        // 这样可以确保自动测试等待者不会一直挂起
        if (!silent && wasPlaying && typeof this.onPlaybackFinished === 'function') {
            try {
                log('📢 重置时触发播放完成回调（清理上一轮播放状态，避免自动测试挂起）', 'warning');
                this.onPlaybackFinished();
            } catch (error) {
                log(`❌ 播放完成回调执行出错: ${error.message}`, 'error');
            }
        }
    }
}

// 创建streamingContext实例的工厂函数
export function createStreamingContext(opusDecoder, audioContext, sampleRate, channels, minAudioDuration) {
    return new StreamingContext(opusDecoder, audioContext, sampleRate, channels, minAudioDuration);
}


