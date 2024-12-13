### 使用说明

```bash
go run main.go --help

Usage of main.go:
  -f, --slowLogFile string         MySQL慢查询日志文件路径 (default "/var/log/mysql/mysql-slow.log")
  -s, --slowQueryThreshold float   慢查询阈值，单位：秒，支持整数或小数 (default 0.5)
  -t, --test                       发送一个测试WebHook请求
  -u, --webhookURL string          Webhook URL 用于发送通知
pflag: help requested
exit status 2
```

### 使用示例

```bash
# 使用帮助
./mysql-slow-sql-webhook --help
# 测试发送
./mysql-slow-sql-webhook -u https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxxx -t
# 默认文件路径
./mysql-slow-sql-webhook -u https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxxx
# 指定文件路径
./mysql-slow-sql-webhook -u https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxxx -f /log/mysql/mysql-slow.log
# 设置发送通知超时时间
./mysql-slow-sql-webhook -u https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxxx -f /log/mysql/mysql-slow.log -s 0.2
```
