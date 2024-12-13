package main

import (
	"bufio"
	"fmt"
	"github.com/spf13/pflag"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/go-resty/resty/v2"
)

// 配置命令行参数
var webhookURL string
var slowLogFile string
var slowQueryThreshold float64 // 慢查询阈值，单位：秒
var isTest bool                // 是否发送测试WebHook请求

// 正则表达式，用于提取慢查询日志中的信息
var slowLogPattern = regexp.MustCompile(`(?s)Query_time:\s*(\d+\.\d+|\d+)\s*Lock_time:\s*(\d+\.\d+|\d+)\s*Rows_sent:\s*(\d+)\s*Rows_examined:\s*(\d+)\s*Database:\s*(\S+)\s*User:\s*(\S+)\s*Query:\s*(.*)`)

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
func processSlowQuery(logLine string) {
	// 匹配日志行
	matches := slowLogPattern.FindStringSubmatch(logLine)
	if matches != nil {
		// 提取信息
		queryTime, _ := strconv.ParseFloat(matches[1], 64) // 查询时间（秒）
		lockTime, _ := strconv.ParseFloat(matches[2], 64)  // 锁定时间
		rowsSent, _ := strconv.Atoi(matches[3])            // 发送的行数
		rowsExamined, _ := strconv.Atoi(matches[4])        // 扫描的行数
		database := matches[5]                             // 数据库
		user := matches[6]                                 // 用户
		sqlQuery := matches[7]                             // 查询语句

		// 判断是否是慢查询
		if queryTime >= slowQueryThreshold {
			// 构造 Markdown 格式的通知内容
			notificationContent := fmt.Sprintf(
				`<font color=\"warning\">**慢查询警告测试**</font>\n`+
					`> **查询时间:** <font color=\"warning\">%.2f 秒</font>\n`+
					`> **锁定时间:** <font color=\"comment\">%.2f 秒</font>\n`+
					`> **数据库:** <font color=\"comment\">%s</font>\n`+
					`> **用户:** <font color=\"comment\">%s</font>\n`+
					`> **SQL查询:** <font color=\"comment\">%s</font>\n`+
					`> **发送的行数:** <font color=\"comment\">%d</font>\n`+
					`> **扫描的行数:** <font color=\"comment\">%d</font>\n`,
				queryTime, lockTime, database, user, sqlQuery, rowsSent, rowsExamined)

			// 发送 Webhook 通知
			sendWebhookNotification(notificationContent)
		}
	}
}

// 实时读取MySQL慢查询日志
func tailSlowLog() {
	file, err := os.Open(slowLogFile)
	if err != nil {
		fmt.Printf("无法打开慢查询日志文件: %v\n", err)
		return
	}
	defer file.Close()

	// 使用Scanner逐行读取日志
	scanner := bufio.NewScanner(file)
	for {
		// 实时读取新增的日志内容
		if scanner.Scan() {
			line := scanner.Text()
			processSlowQuery(line)
		} else if err := scanner.Err(); err != nil {
			fmt.Printf("读取日志时出错: %v\n", err)
			time.Sleep(1 * time.Second) // 等待1秒后重试
		}
	}
}

// 发送一个测试 Webhook 请求
func sendTestWebhook() {
	// 构造一个简单的测试消息
	testMessage := fmt.Sprintf(
		`<font color=\"warning\">**慢查询警告测试**</font>\n`+
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

	// 开始监控MySQL慢查询日志
	fmt.Println("开始监控MySQL慢查询日志...")
	tailSlowLog()
}
