package main

import (
	"fmt"
	"github.com/go-resty/resty/v2"
	"github.com/hpcloud/tail"
	"github.com/spf13/pflag"
	"regexp"
	"strconv"
	"sync"
)

// 配置命令行参数
var webhookURL string
var slowLogFile string
var slowQueryThreshold float64 // 慢查询阈值，单位：秒
var isTest bool                // 是否发送测试WebHook请求
var readHistory bool           // 是否读取历史日志数据，默认为 false

// 正则表达式，用于提取慢查询日志中的信息
var queryStartPattern = regexp.MustCompile(`^# Time: \d{4}-\d{2}-\d{2}.*$`)
var queryTimePattern = regexp.MustCompile(`# Query_time:\s*(\d+\.\d+|\d+)\s*Lock_time:\s*(\d+\.\d+|\d+)\s*Rows_sent:\s*(\d+)\s*Rows_examined:\s*(\d+)`)
var userHostPattern = regexp.MustCompile(`# User@Host:\s*(\S+)\s*\[\S+\]\s*@\s*(\S+)`)
var databasePattern = regexp.MustCompile(`# Schema:\s*(\S+)`) // 匹配数据库名
var sqlQueryEndPattern = regexp.MustCompile(`(?i)^(SELECT|UPDATE|DELETE|INSERT)\s+.*;$`)

// 发送Webhook通知
func sendWebhookNotification(content string) {
	payload := fmt.Sprintf(`{
		"msgtype": "markdown",
		"markdown": {
			"content": "%s"
		}
	}`, content)

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

	for _, line := range logLines {
		if matches := queryTimePattern.FindStringSubmatch(line); matches != nil {
			queryTime, _ = strconv.ParseFloat(matches[1], 64)
			lockTime, _ = strconv.ParseFloat(matches[2], 64)
			rowsSent, _ = strconv.Atoi(matches[3])
			rowsExamined, _ = strconv.Atoi(matches[4])
		}
		if matches := userHostPattern.FindStringSubmatch(line); matches != nil {
			user = matches[1]
			host = matches[2]
		}
		if matches := databasePattern.FindStringSubmatch(line); matches != nil {
			database = matches[1]
		}
		if matches := sqlQueryEndPattern.FindStringSubmatch(line); matches != nil {
			sqlQuery = line
		}
	}

	if queryTime >= slowQueryThreshold {
		notificationContent := fmt.Sprintf(
			`<font color=\"warning\">**慢查询警告**</font>\n`+
				`> **查询时间:** <font color=\"warning\">%.2f 秒</font>\n`+
				`> **锁定时间:** <font color=\"comment\">%.2f 秒</font>\n`+
				`> **数据库:** <font color=\"comment\">%s</font>\n`+
				`> **主机:** <font color=\"comment\">%s</font>\n`+
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
	defer wg.Done()

	t, err := tail.TailFile(slowLogFile, tail.Config{
		Follow:    true, // 实时跟踪文件变化
		ReOpen:    true, // 支持文件轮转
		MustExist: true, // 文件必须存在
		Poll:      true, // 使用轮询模式
	})
	if err != nil {
		fmt.Printf("无法跟踪慢查询日志文件: %v\n", err)
		restart <- true
		return
	}

	var logLines []string
	for line := range t.Lines {
		// 读取每一行日志
		if line.Text == "" {
			continue
		}

		if queryStartPattern.MatchString(line.Text) {
			if len(logLines) > 0 {
				processSlowQuery(logLines) // 处理当前完整日志条目
			}
			logLines = []string{line.Text} // 初始化新的日志条目
		} else {
			logLines = append(logLines, line.Text)
		}

		if sqlQueryEndPattern.MatchString(line.Text) {
			processSlowQuery(logLines) // 处理完整的日志条目
			logLines = nil             // 清空已处理的日志
		}

		// 处理日志的最后剩余部分（文件结束时未处理的部分）
		if len(logLines) > 0 {
			processSlowQuery(logLines)
		}
	}
}

func main() {
	pflag.StringVarP(&webhookURL, "webhookURL", "u", "", "Webhook URL 用于发送通知")
	pflag.StringVarP(&slowLogFile, "slowLogFile", "f", "/var/log/mysql/mysql-slow.log", "MySQL慢查询日志文件路径")
	pflag.Float64VarP(&slowQueryThreshold, "slowQueryThreshold", "s", 0.5, "慢查询阈值，单位：秒")
	pflag.BoolVarP(&isTest, "test", "t", false, "发送一个测试WebHook请求")
	pflag.BoolVarP(&readHistory, "readHistory", "r", false, "是否读取历史日志数据")
	pflag.Parse()

	if webhookURL == "" {
		fmt.Println("Webhook URL 必须设置！")
		pflag.Usage()
		return
	}

	fmt.Printf("Webhook URL: %s\n", webhookURL)
	fmt.Printf("慢查询日志文件: %s\n", slowLogFile)
	fmt.Printf("慢查询阈值: %.2f 秒\n", slowQueryThreshold)
	fmt.Printf("读取历史日志数据: %v\n", readHistory)

	var wg sync.WaitGroup
	restart := make(chan bool)

	for {
		wg.Add(1)
		go tailSlowLog(&wg, restart)
		select {
		case <-restart:
			fmt.Println("日志监控协程退出，正在重新启动...")
		}
	}

	wg.Wait()
}
