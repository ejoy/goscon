# gosconn

## 特性
* 断线重连: [scp协议介绍](https://github.com/ejoy/goscon/blob/master/protocol.md)
* 加密： [dh64密钥交换](https://en.wikipedia.org/wiki/Diffie%E2%80%93Hellman_key_exchange)及对称流加密
* 负载均衡
* 命名服务路由
* 配置热更新
* 支持`kcp`和`tcp`，且可以无缝切换

## 用法

断线重连服务器端代理。

```
client <--> goscon <---> server
```

`client`和`goscon`之间使用断线重连协议，`goscon`把客户端的请求内容转发到`server`。

无论`client`因为何种原因主动或被动断开连接，`goscon`都会维持对应的`server`连接，使`server`感受不到`client`断开。

在`goscon`维持连接期间，`client`可以使用断线重连协议，无缝重用之前的连接。

若`scp.reuse_time`秒没有被重用，`goscon`断开跟`server`的连接。

编译时开启`sproto`扩展，新建连接后自动给后端发送一条`sproto`消息，宣布客户端的原始`ip`地址信息。

## build & run & test

* deps: go v1.13+

* build
```bash
# normal compile
go build -mod=vendor

# enable sproto hook & debug
# go build -tags sproto,debug -mod=vendor

```

* config

配置选项含义，请参考[config.go](https://github.com/ejoy/goscon/blob/master/config.go)

* run
```bash
./goscon -logtostderr -v 10 -config config.yaml
```

* test

- 编译测试程序

```bash
# normal compile
go build -mod=vendor ./examples/client
```

- 启动服务端

```bash
./client.exe -startEchoServer :11248
```

- 测试 tcp

```
./client.exe -packets 10 -concurrent 100 -rounds 100
```

- 测试 kcp

```
./client.exe kcp
```

## maintenance

可以通过默认开启的管理端口`http://localhost:6220`进行配置热更新，查看内部状态。

* 热更新配置
    - 修改配置文件
    - 访问: `http://localhost:6220/reload`
* 查看内部状态
    - 当前配置：`http://localhost:6220/config`
    - 状态: `http://localhost:6220/status`
    - kcp snmp: `http://localhost:6220/kcp/snmp`