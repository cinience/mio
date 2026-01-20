import { log } from './utils/logger.js';
import { otaStatusStyle } from './document.js'

// WebSocket 连接
export async function webSocketConnect(otaUrl, wsUrl, config, options = {}){
    console.log('[webSocketConnect] start', {
        otaUrl,
        wsUrl,
        deviceId: config?.deviceId,
        clientId: config?.clientId
    });
    log('开始连接流程，准备请求OTA接口获取服务器地址...', 'info');

    if (!validateConfig(config)) {
        console.warn('[webSocketConnect] invalid config');
        return;
    }

    let otaResponse = await sendOTA(otaUrl, config);
    if (!otaResponse) {
        log('OTA请求未返回有效数据，无法建立连接', 'error');
        console.error('[webSocketConnect] OTA response invalid or empty');
        return;
    }
    console.log('[webSocketConnect] OTA response received', otaResponse);
    log('OTA接口响应成功，解析WebSocket地址中...', 'info');

    const {
        onActivationRequired,
        onActivationPolling,
        onActivationSuccess,
        onActivationFailed
    } = options || {};

    if (otaResponse?.activation) {
        log('检测到设备尚未激活，开始激活流程...', 'warning');
        console.log('[webSocketConnect] activation payload received', otaResponse.activation);

        if (typeof onActivationRequired === 'function') {
            try {
                await onActivationRequired({
                    ...otaResponse.activation,
                    deviceId: config.deviceId,
                    clientId: config.clientId
                });
            } catch (error) {
                log('用户取消激活流程，终止连接', 'warning');
                console.warn('[webSocketConnect] activation flow cancelled by user', error);
                return null;
            }
        } else {
            log(
                `请在激活站点完成绑定，激活码: ${otaResponse.activation.code}`,
                'warning'
            );
        }

        const activated = await waitForActivation(otaUrl, config, otaResponse.activation, {
            onActivationPolling,
            onActivationFailed
        });

        if (!activated) {
            log('激活流程未完成，终止连接', 'error');
            console.error('[webSocketConnect] activation not completed');
            return null;
        }

        if (typeof onActivationSuccess === 'function') {
            onActivationSuccess();
        }

        log('设备激活成功，重新获取OTA信息...', 'info');
        otaResponse = await sendOTA(otaUrl, config);
        if (!otaResponse) {
            log('激活后重新获取OTA信息失败', 'error');
            console.error('[webSocketConnect] failed to refetch OTA after activation');
            return null;
        }
        console.log('[webSocketConnect] OTA response after activation', otaResponse);
        if (otaResponse?.activation) {
            log('激活后仍返回激活信息，请稍后重试', 'error');
            console.error('[webSocketConnect] activation still required after success', otaResponse.activation);
            return null;
        }
    }

    let targetWsUrl = (wsUrl || '').trim();
    if (!targetWsUrl) {
        const suggestedUrl = otaResponse?.websocket?.url;
        if (suggestedUrl && typeof suggestedUrl === 'string' && suggestedUrl.trim() !== '') {
            targetWsUrl = suggestedUrl.trim();
            log(`从OTA响应中获取WebSocket地址: ${targetWsUrl}`, 'info');
            console.log('[webSocketConnect] using OTA websocket url', targetWsUrl);
        } else {
            log('OTA响应中未提供有效的WebSocket地址，请手动填写', 'error');
            console.error('[webSocketConnect] missing websocket url in OTA response');
            return;
        }
    }

    if (!validateWsUrl(targetWsUrl)) {
        console.warn('[webSocketConnect] websocket url validation failed', targetWsUrl);
        return;          // 直接返回，不再往下执行
    }

    // 使用自定义WebSocket实现以添加认证头信息
    let connUrl;
    try {
        connUrl = new URL(targetWsUrl);
    } catch (error) {
        log(`WebSocket地址无效: ${targetWsUrl}`, 'error');
        console.error('[webSocketConnect] invalid websocket url', targetWsUrl, error);
        return;
    }
    // 添加认证参数
    connUrl.searchParams.append('device-id', config.deviceId);
    connUrl.searchParams.append('client-id', config.clientId);
    log(`正在连接: ${connUrl.toString()}`, 'info');
    console.log('[webSocketConnect] final connection url', connUrl.toString());

    const socket = new WebSocket(connUrl.toString());
    console.log('[webSocketConnect] websocket instance created');

    return {
        socket,
        url: targetWsUrl
    };
}

// 验证配置
function validateConfig(config) {
    if (!config.deviceMac) {
        log('设备MAC地址不能为空', 'error');
        return false;
    }
    if (!config.clientId) {
        log('客户端ID不能为空', 'error');
        return false;
    }
    return true;
}

// 判断wsUrl路径是否存在错误
function validateWsUrl(wsUrl){
    if (wsUrl === '') return false;
    // 检查URL格式
    if (!wsUrl.startsWith('ws://') && !wsUrl.startsWith('wss://')) {
        log('URL格式错误，必须以ws://或wss://开头', 'error');
        return false;
    }
    return true
}


// OTA发送请求，验证状态
async function sendOTA(otaUrl, config) {
    if (!otaUrl || otaUrl.trim() === '') {
        log('OTA地址不能为空', 'error');
        otaStatusStyle(false);
        console.error('[webSocketConnect] ota url is empty');
        return null;
    }
    try {
        log(`请求OTA接口: ${otaUrl}`, 'info');
        console.log('[webSocketConnect] sending OTA request', { otaUrl, deviceId: config.deviceId, clientId: config.clientId });

        const payload = {
            version: 0,
            uuid: '',
            application: {
                name: 'xiaozhi-web-test',
                version: '1.0.0',
                compile_time: '2025-04-16 10:00:00',
                idf_version: '4.4.3',
                elf_sha256: '1234567890abcdef1234567890abcdef1234567890abcdef'
            },
            ota: { label: 'xiaozhi-web-test' },
            board: {
                type: 'xiaozhi-web-test',
                ssid: 'xiaozhi-web-test',
                rssi: 0,
                channel: 0,
                ip: '192.168.1.1',
                mac: config.deviceMac
            },
            flash_size: 0,
            minimum_free_heap_size: 0,
            mac_address: config.deviceMac,
            chip_model_name: '',
            chip_info: { model: 0, cores: 0, revision: 0, features: 0 },
            partition_table: [{ label: '', type: 0, subtype: 0, address: 0, size: 0 }]
        };

        log(`OTA请求Payload:\n${JSON.stringify(payload, null, 2)}`, 'info');

        const res = await fetch(otaUrl, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Device-Id': config.deviceId,
                'Client-Id': config.clientId
            },
            body: JSON.stringify(payload)
        });

        if (!res.ok) {
            const errorText = await res.text();
            log(`OTA请求失败，状态码: ${res.status} ${res.statusText}`, 'error');
            if (errorText) {
                log(`OTA响应内容: ${errorText}`, 'error');
            }
            console.error('[webSocketConnect] OTA request non-200', res.status, res.statusText, errorText);
            throw new Error(`${res.status} ${res.statusText}`);
        }

        console.log('[webSocketConnect] OTA request completed', res.status, res.statusText);
        const responseText = await res.text();
        log(`OTA原始响应:\n${responseText || '(空响应)'}`, 'info');

        let result;
        try {
            result = responseText ? JSON.parse(responseText) : {};
        } catch (parseError) {
            log(`OTA响应解析失败: ${parseError.message}`, 'error');
            console.error('[webSocketConnect] OTA response parse error', parseError);
            otaStatusStyle(false);
            return null;
        }

        console.log('[webSocketConnect] OTA response payload', result);
        log(`OTA响应数据:\n${JSON.stringify(result, null, 2)}`, 'info');
        if (result && result.error) {
            log(`OTA请求返回错误: ${result.error}`, 'error');
            otaStatusStyle(false);
            console.error('[webSocketConnect] OTA request returned error', result.error);
            return null;
        }
        otaStatusStyle(true)
        console.log('[webSocketConnect] OTA request succeeded');
        log('OTA接口请求成功', 'success');
        if (result?.websocket?.url) {
            log(`OTA返回的WebSocket地址: ${result.websocket.url}`, 'info');
        }
        return result; // 返回OTA结果
    } catch (err) {
        log(`OTA请求失败: ${err.message}`, 'error');
        otaStatusStyle(false)
        console.error('[webSocketConnect] OTA request failed', err);
        return null; // 失败
    }
}

async function waitForActivation(otaUrl, config, activationData, callbacks = {}) {
    const {
        onActivationPolling,
        onActivationFailed
    } = callbacks || {};

    const pollIntervalMs = 5000;
    const maxAttempts = 120; // 约10分钟
    const activationUrl = buildActivationUrl(otaUrl);

    log('开始轮询检查设备激活状态', 'info');
    console.log('[webSocketConnect] start activation polling', {
        activationUrl,
        deviceId: config.deviceId,
        clientId: config.clientId
    });

    for (let attempt = 1; attempt <= maxAttempts; attempt++) {
        try {
            if (typeof onActivationPolling === 'function') {
                onActivationPolling({
                    attempt,
                    status: 'checking',
                    retryInSeconds: pollIntervalMs / 1000
                });
            }

            const response = await fetch(activationUrl, {
                method: 'POST',
                headers: {
                    'Content-Type': 'application/json',
                    'Device-Id': config.deviceId,
                    'Client-Id': config.clientId || config.deviceId
                },
                body: JSON.stringify({
                    Payload: {
                        algorithm: 'check-only',
                        serial_number: config.deviceId,
                        challenge: activationData?.challenge || config.deviceId
                    }
                })
            });

            console.log('[webSocketConnect] activation poll response', {
                status: response.status,
                attempt
            });

            if (response.status === 200) {
                log('服务器确认设备已激活', 'success');
                if (typeof onActivationPolling === 'function') {
                    onActivationPolling({
                        attempt,
                        status: 'activated',
                        retryInSeconds: 0
                    });
                }
                return true;
            }

            if (response.status !== 202) {
                const text = await response.text();
                log(`激活检查返回状态: ${response.status} ${text || ''}`.trim(), 'warning');
            } else {
                log('等待用户完成激活...', attempt === 1 ? 'info' : 'debug');
            }
        } catch (error) {
            log(`激活检查失败: ${error.message}`, 'warning');
            console.error('[webSocketConnect] activation polling error', error);
        }

        await sleep(pollIntervalMs);
    }

    log('未在预期时间内完成激活', 'error');
    if (typeof onActivationFailed === 'function') {
        onActivationFailed();
    }
    return false;
}

function buildActivationUrl(otaUrl) {
    try {
        const url = new URL(otaUrl);
        if (!url.pathname.endsWith('/')) {
            url.pathname += '/';
        }
        url.pathname += 'activate';
        return url.toString();
    } catch (error) {
        console.warn('[webSocketConnect] invalid OTA url during activation url build', otaUrl, error);
        const suffix = otaUrl.endsWith('/') ? '' : '/';
        return `${otaUrl}${suffix}activate`;
    }
}

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}
