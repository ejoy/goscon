gosconn
=======
断线重连服务器端实现


新建连接
========

Client->Server: 传输一个 2 byte size(big-endian) + content 的包. (size == #content) 包的内容如下:

0\n
base64(DH_key)\n

这里, DH-key 是一个 8bytes 的, 经过 DH 算法计算出来的 key . 可以利用 lua-crypt 库 :
https://github.com/cloudwu/skynet/blob/dev/lualib-src/lua-crypt.c

local clientkey = crypt.randomkey()	-- 随机产生一个 64bit number, 并记录下来.
local dh_key = crypt.base64encode(crypt.dhexchange(clientkey)))

Server->Client: 回应给 Client 一个握手信息:
id\n
base64(DH_key)\n

这里, id 是一个 10 进制的非 0 数字串. 建议在 [1,2^32) 之间. 因为实现可能利用 uint32_t 保存这个 id .
DH_key 的算法同 client 的算法.


握手完毕后, 双方获得一个公有的 64bit secret,  计算方法为:

local secret = crypt.dhsecret(crypt.base64decode(收到的 key), 事先保存的 key)

恢复连接
========

Client->Server: 传输一个 2 byte size(big-endian) + content 的包. (size == #content) 包的内容如下:

id\n
index\n
recvnumber\n
base64(HMAC_CODE)\n

这里 id 为新建连接时, 服务器交给 Client 的 id .
index 是一个从 1 开始(第一次恢复为 1), 递增的十进制数字. 服务器会拒绝重复使用过的数字.
recvnumber 是一个 10 进制数字, 表示 (曾经从服务器收到多少字节 mod 2^32).
把以上三行放在一起(保留 \n) content, 以及在新建连接时交换得到的 serect, 计算一个 HMAC_CODE, 算法是:

HMAC_CODE = crypt.hmac64(crypt.hashkey(content), secret)

Server->Client: 回应握手消息:

recvnumber\n
CODE msg\n

这里, recvnumber 是一个 10 进制数字, 表示 (曾经在这个会话上, 服务器收到过客户端发出的多少字节 mod 2^32).
CODE 是一个10进制三位数, 表示连接是否恢复. msg 是具体信息:

200 OK
	表示连接成功
401 Unauthorized
	表示 HMAC 计算错误
403 Index Expired
	表示 Index 已经使用过
404 User Not Found
	表示连接 id 已经无效
406 Not Acceptable
	表示 cache 的数据流不够

------

当连接恢复后, 服务器应当根据之前记录的发送出去的字节数（不计算每次握手包的字节）, 减去客户端通知它收到的字节数, 开始补发未收到的字节。
客户端也做相同的事情。
