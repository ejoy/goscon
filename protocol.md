# Stable Connection Protocol
## 介绍
为了解决移动场景下，网络闪断问题而订立的复用连接协议。

## 协议

### 新建连接

Client->Server: 传输一个 2 byte size(big-endian) + content 的包, size == len(content)

包的内容如下:

```
0\n
base64(DHPublicKey)\n
targetServer\n
flag
```

`DHPublicKey` 是一个 8 bytes 值, 经过 DH 算法计算出来的 key。

`targetServer`用于提示优先连接的后端服务器名字。`targetServer`应该仅包括[a-zA-Z_0-9]。

`flag` 32 位整形，允许通过设置不同的比特位开启不同的选项。

- 比特位 1: 表示禁止将 client ip 发送给 upstream。
- ...

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
* 400 Malformed request : 数据解释失败
* 401 Unauthorized : 表示 HMAC 计算错误
* 403 Index Expired : 表示 Index 已经使用过
* 404 User Not Found : 表示连接 id 已经无效
* 406 Not Acceptable : 表示 cache 的数据流不够
* 501 Network Error ：网络相关错误

当连接恢复后, 服务器应当根据之前记录的发送出去的字节数（不计算每次握手包的字节）, 减去客户端通知它收到的字节数, 开始补发未收到的字节。
客户端也做相同的事情。