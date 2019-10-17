# gosconn

断线重连服务器端代理。

```
client <--> goscon <---> server
```

client和goscon之间使用断线重连协议，goscon把客户端的请求内容，转发到server。

编译时开启`sproto`扩展，可以新建连接后自动给后端发送一条`sproto`消息，宣布客户端的IP地址信息。

## build & run

* deps: go v1.13+

```bash
go build -vendor=mod
```

启动tcp网关：
```bash
./goscon -listen="0.0.0.0:1234" -config="/path/to/conf" -tcp=""
```

启动kcp网关:
```bash
./goscon -listen="0.0.0.0:1234" -config="/path/to/conf" -kcp="fec_data:0,fec_parity:0"
```

同时启动：
```bash
./goscon -listen="0.0.0.0:1234" -config="/path/to/conf" -tcp="" -kcp="fec_data:0,fec_parity:0"
```

## 协议

### 新建连接

Client->Server: 传输一个 2 byte size(big-endian) + content 的包, size == len(content)

包的内容如下:

```
0\n
base64(DHPublicKey)\n
targetServer
```

DHPublicKey 是一个 8 bytes 值, 经过 DH 算法计算出来的 key。

`targetServer`用于提示优先连接的后端服务器名字。`targetServer`应该仅包括[a-zA-Z_0-9]。

```
DHPrivateKey = dh64.PrivateKey()
DHPublicKey = dh64.PublicKey(DHPrivateKey)
```

Server->Client: 回应给 Client 一个握手信息:

```
id\n
base64(DHPublicKey)
```

这里, id 是一个 10 进制的非 0 数字串. 建议在 [1,2^32) 之间. 因为实现可能利用 uint32_t 保存这个 id .

DHPublicKey 的算法同 client 的算法.

握手完毕后, 双方获得一个公有的 64bit secret,  计算方法为:

```
secret = dh64.Secret(myDHPrivateKey, otherDHPublicKey)
```

### 恢复连接

Client->Server: 传输一个 2 byte size(big-endian) + content 的包, size == len(content)

包的内容如下:

```
id\n
index\n
recvnumber\n
base64(HMAC_CODE)
```

这里 id 为新建连接时, 服务器交给 Client 的 id .

index 是一个从 1 开始(第一次恢复为 1), 递增的十进制数字. 服务器会拒绝重复使用过的数字.

recvnumber 是一个 10 进制数字, 表示 (曾经从服务器收到多少字节 mod 2^32).

把以上三行放在一起(保留 \n) content, 以及在新建连接时交换得到的 serect, 计算一个 HMAC_CODE, 算法是:

HMAC_CODE = crypt.hmac64(crypt.hashkey(content), secret)

Server->Client: 回应握手消息:

```
recvnumber\n
CODE msg
```

这里, recvnumber 是一个 10 进制数字, 表示 (曾经在这个会话上, 服务器收到过客户端发出的多少字节 mod 2^32).
CODE 是一个10进制三位数, 表示连接是否恢复:

* 200 OK : 表示连接成功
* 401 Unauthorized : 表示 HMAC 计算错误
* 403 Index Expired : 表示 Index 已经使用过
* 404 User Not Found : 表示连接 id 已经无效
* 406 Not Acceptable : 表示 cache 的数据流不够

当连接恢复后, 服务器应当根据之前记录的发送出去的字节数（不计算每次握手包的字节）, 减去客户端通知它收到的字节数, 开始补发未收到的字节。
客户端也做相同的事情。

## kcp 选项

kcp 的配置分为两部分，通过配置文件传入和通过命令行参数传入。配置文件传入的主要是以下几项，参考 settings.conf 中配置项。**注意：配置文件中的这几项不能省略，如果省略了，默认就是 0** 。 热更新只会影响新建的 kcp 会话。

* mtu               最大传输单元
* rcvWnd            kcp连接的接收窗口
* sndWnd            kcp连接的发送窗口
* nodelay           是否启用 nodelay 模式
* interval          kcp 调用 update 的时间间隔，单位是毫秒
* resend            快速重换模式，比如 2 表示：2 次 ACK 跨越直接重传
* nc                是否关闭拥塞控制，0-开启，1-关闭

命令行参数传入的主要是以下几项：

* readTimeout       接收数据超时时间，单位为秒，默认 60 秒，超过 60 秒没有收到数据认为连接断开
* read_buffer       udp socket 的 RCV_BUF，单位为字节，默认为 4M
* write_buffer      udp socket 的 SND_BUF，单位为字节，默认为 4M
* reuseport         利用端口复用的特性，同时开启多个 Goroutine 监听端口，默认为 8
* fec_data          fec 参数
* fec_parity        fec 参数
* snmplog           记录 snmp 日志的目录，如：./snmp-20060102.log，需要保证目录存在。如果传空，则不打印。
* snmpperiod        记录 snmp 日志的间隔，默认为 60 秒
