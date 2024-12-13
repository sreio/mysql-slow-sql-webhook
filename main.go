package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/spf13/pflag"
)

// 配置命令行参数
var webhookURL string
var slowLogFile string
var slowQueryThreshold float64 // 慢查询阈值，单位：秒
var isTest bool                // 是否发送测试WebHook请求
var readHistory bool           // 是否读取历史日志数据，默认为 false

// 正则表达式，用于提取慢查询日志中的信息
var queryTimePattern = regexp.MustCompile(`# Query_time:\s*(\d+\.\d+|\d+)\s*Lock_time:\s*(\d+\.\d+|\d+)\s*Rows_sent:\s*(\d+)\s*Rows_examined:\s*(\d+)`)
var userHostPattern = regexp.MustCompile(`# User@Host:\s*(\S+)\s*\[\S+\]\s*@\s*(\S+)`)
var databasePattern = regexp.MustCompile(`# Database:\s*(\S+)`) // 匹配数据库名
var sqlQueryPattern = regexp.MustCompile(`(SELECT|INSERT|UPDATE|DELETE|REPLACE|CREATE|DROP|ALTER|TRUNCATE)\s+.+;`)

// 发送Webhook通知
func sendWebhookNotification(content string) {
	// 构造Webhook数据，格式化为Markdown类型
	payload := fmt.Sprintf(`{
		"msgtype": "markdown",
		"markdown": {
			"content": "%s"
		}
	}`, content)

	// 使用 resty 发送 POST 请求
	client := resty.New()
	_, err := client.R().
		SetHeader("Content-Type", "application/json").
		SetBody(payload).
		Post(webhookURL)

	if err != nil {
		fmt.Printf("发送Webhook通知失败: %v\n", err)
	} else {
		fmt.Println("Webhook通知已发送")
	}
}

// 解析慢查询日志并判断是否是慢查询
func processSlowQuery(logLines []string) {
	// 变量声明
	var queryTime float64
	var lockTime float64
	var rowsSent int
	var rowsExamined int
	var database string
	var user string
	var host string
	var sqlQuery string

	// 遍历每一行日志信息
	for _, line := range logLines {
		// 匹配 Query_time, Lock_time, Rows_sent, Rows_examined
		if matches := queryTimePattern.FindStringSubmatch(line); matches != nil {
			queryTime, _ = strconv.ParseFloat(matches[1], 64)
			lockTime, _ = strconv.ParseFloat(matches[2], 64)
			rowsSent, _ = strconv.Atoi(matches[3])
			rowsExamined, _ = strconv.Atoi(matches[4])
		}

		// 匹配 User@Host
		if matches := userHostPattern.FindStringSubmatch(line); matches != nil {
			user = matches[1]
			host = matches[2]
		}

		// 匹配 Database
		if matches := databasePattern.FindStringSubmatch(line); matches != nil {
			database = matches[1]
		}

		// 获取 SQL 查询语句
		if matches := sqlQueryPattern.FindStringSubmatch(line); matches != nil {
			sqlQuery = line
		}
	}

	// 判断是否是慢查询
	if queryTime >= slowQueryThreshold {
		// 构造 Markdown 格式的通知内容
		notificationContent := fmt.Sprintf(
			`<font color=\"warning\">**慢查询警告**</font>\n`+
				`> **查询时间:** <font color=\"warning\">%.2f 秒</font>\n`+
				`> **锁定时间:** <font color=\"comment\">%.2f 秒</font>\n`+
				`> **数据库:** <font color=\"comment\">%s</font>\n`+
				`> **host:** <font color=\"comment\">%s</font>\n`+
				`> **用户:** <font color=\"comment\">%s</font>\n`+
				`> **发送的行数:** <font color=\"comment\">%d</font>\n`+
				`> **扫描的行数:** <font color=\"comment\">%d</font>\n`+
				`> **SQL 查询:** <font color=\"comment\">%s</font>\n`,
			queryTime, lockTime, database, host, user, rowsSent, rowsExamined, sqlQuery)
		
		// 发送 Webhook 通知
		sendWebhookNotification(notificationContent)
	}
}

// 实时读取MySQL慢查询日志
func tailSlowLog(wg *sync.WaitGroup, restart chan bool) {
	defer wg.Done() // 确保函数完成时通知 WaitGroup

	for {
		file, err := os.Open(slowLogFile)
		if err != nil {
			fmt.Printf("无法打开慢查询日志文件: %v\n", err)
			restart <- true // 发生错误时通知主程序重新启动协程
			return
		}
		defer file.Close()

		// 根据 readHistory 参数决定是否读取历史数据
		if readHistory {
			// 如果需要读取历史数据，从文件开头开始读取
			file.Seek(0, io.SeekStart) // 将文件指针移动到文件的开头
		} else {
			// 默认只读取最新的日志内容，将文件指针移动到文件末尾
			file.Seek(0, io.SeekEnd) // 将文件指针移动到文件末尾
		}

		// 使用Scanner逐行读取日志
		scanner := bufio.NewScanner(file)
		var logLines []string
		for {
			// 实时读取新增的日志内容
			if scanner.Scan() {
				line := scanner.Text()
				// 如果是空行或非日志行，跳过
				if len(logLines) >= 6 {
					// 一旦读取到 SQL 查询语句，处理整个日志条目
					processSlowQuery(logLines)
					logLines = nil // 清空已处理的日志
					logLines = append(logLines, line)
					continue
				}

				logLines = append(logLines, line)
			} else if err := scanner.Err(); err != nil {
				// 如果读取出错，则输出错误并等待 1 秒重试
				fmt.Printf("读取日志时出错: %v\n", err)
				time.Sleep(1 * time.Second) // 等待1秒后重试
				break                       // 跳出当前循环，重新启动协程
			} else {
				time.Sleep(1 * time.Second) // 等待1秒后重试
			}
		}
	}
}

// 发送一个测试 Webhook 请求
func sendTestWebhook() {
	// 构造一个简单的测试消息
	testMessage := fmt.Sprintf(
		`<font color=\"warning\">**慢查询警告测试示例**</font>\n`+
			`> **查询时间:** <font color=\"warning\">%.2f 秒</font>\n`+
			`> **锁定时间:** <font color=\"comment\">%.2f 秒</font>\n`+
			`> **数据库:** <font color=\"comment\">%s</font>\n`+
			`> **用户:** <font color=\"comment\">%s</font>\n`+
			`> **SQL查询:** <font color=\"comment\">%s</font>\n`+
			`> **发送的行数:** <font color=\"comment\">%d</font>\n`+
			`> **扫描的行数:** <font color=\"comment\">%d</font>\n`,
		0.5, 0.5, "database", "user", "select * from table", 1, 10000)

	// 发送 Webhook 通知
	sendWebhookNotification(testMessage)
}

func main() {
	// 配置命令行参数
	pflag.StringVarP(&webhookURL, "webhookURL", "u", "", "Webhook URL 用于发送通知")
	pflag.StringVarP(&slowLogFile, "slowLogFile", "f", "/var/log/mysql/mysql-slow.log", "MySQL慢查询日志文件路径")
	pflag.Float64VarP(&slowQueryThreshold, "slowQueryThreshold", "s", 0.5, "慢查询阈值，单位：秒，支持整数或小数")
	pflag.BoolVarP(&isTest, "test", "t", false, "发送一个测试WebHook请求")
	pflag.BoolVarP(&readHistory, "readHistory", "r", false, "是否读取历史日志数据")
	pflag.Parse()

	// 检查必选参数是否设置
	if webhookURL == "" {
		fmt.Println("Webhook URL 必须设置！")
		pflag.Usage()
		return
	}

	// 如果是测试模式，发送测试消息并退出
	if isTest {
		fmt.Println("发送测试 Webhook 请求...")
		sendTestWebhook()
		return
	}

	// 打印配置信息
	fmt.Printf("Webhook URL: %s\n", webhookURL)
	fmt.Printf("慢查询日志文件: %s\n", slowLogFile)
	fmt.Printf("慢查询阈值: %.2f 秒\n", slowQueryThreshold)
	fmt.Printf("读取历史日志数据: %v\n", readHistory)

	// 创建一个 WaitGroup 来等待后台任务完成
	var wg sync.WaitGroup
	restart := make(chan bool)

	// 启动一个协程来处理日志文件
	for {
		wg.Add(1)                    // 增加一个等待任务
		go tailSlowLog(&wg, restart) // 在后台运行 tailSlowLog

		// 等待协程退出，并在其退出时重新启动
		select {
		case <-restart:
			fmt.Println("日志监控协程退出，正在重新启动...")
		}
	}

	// 等待所有后台任务完成
	wg.Wait()
}
