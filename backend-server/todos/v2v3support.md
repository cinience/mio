我需要为 Golang WebSocket 服务器添加 XiaoZhi 协议 v2 和 v3 版本支持。  
  
## 背景  
当前服务器只支持 v1 版本(直接接收 Opus 音频数据)。需要添加对 v2 和 v3 版本的支持。  
  
## 协议规范  
  
### 版本 2 - BinaryProtocol2  
二进制帧格式(16字节头 + payload):  
```go  
type BinaryProtocol2 struct {  
    Version     uint16  // 协议版本,网络字节序  
    Type        uint16  // 消息类型: 0=OPUS, 1=JSON,网络字节序  
    Reserved    uint32  // 保留字段  
    Timestamp   uint32  // 时间戳(毫秒),用于AEC,网络字节序  
    PayloadSize uint32  // 负载大小(字节),网络字节序  
    Payload     []byte  // 实际音频数据  
}  
版本 3 - BinaryProtocol3
二进制帧格式(4字节头 + payload):

type BinaryProtocol3 struct {  
    Type        uint8   // 消息类型  
    Reserved    uint8   // 保留字段  
    PayloadSize uint16  // 负载大小,网络字节序  
    Payload     []byte  // 实际音频数据  
}
实现要求
握手阶段:
从 WebSocket 握手的 Protocol-Version 请求头读取客户端协议版本
客户端会发送 hello 消息,其中包含 "version": 1/2/3 字段
服务器需要记录该会话使用的协议版本
接收音频数据:
根据协议版本解析二进制帧:
v1: 直接使用整个二进制帧作为 Opus 数据
v2: 解析 16 字节头,提取 timestamp 和 payload
v3: 解析 4 字节头,提取 payload
注意网络字节序转换(big-endian)
发送音频数据:
根据协议版本封装二进制帧:
v1: 直接发送 Opus 数据
v2: 构建 BinaryProtocol2 头 + Opus 数据
v3: 构建 BinaryProtocol3 头 + Opus 数据
使用 encoding/binary 包处理字节序
AEC 支持:
v2 版本的 timestamp 字段用于服务器端回声消除
如果实现 AEC,需要使用该时间戳进行音频对齐
参考实现
参考 ESP32 客户端本地代码(example/xiaozhi-esp32) 的实现逻辑:

发送: main/protocols/websocket_protocol.cc:28-58
接收: main/protocols/websocket_protocol.cc:111-146
协议结构: main/protocols/protocol.h:17-31
请帮我实现一个支持这三个版本的 WebSocket 音频处理模块。

  
## Notes  [header-2](#header-2)
  
这个提示词基于 ESP32 客户端的实现细节 [1](#1-0) 和协议结构定义 [2](#1-1) 。<cite/>关键点包括:网络字节序转换 [3](#1-2) ,协议版本协商 [4](#1-3) ,以及接收时的版本判断逻辑 [5](#1-4) 。<cite/>  
  
Wiki pages you might want to explore:  
- [WebSocket and MQTT Communication (78/xiaozhi-esp32)](/wiki/78/xiaozhi-esp32#3.2)
