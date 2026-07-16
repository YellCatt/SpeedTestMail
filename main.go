package main

import (
	"fmt"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/showwin/speedtest-go/speedtest"
	"gopkg.in/yaml.v3"
)

type Config struct {
	SMTP struct {
		Host           string `yaml:"host"`
		Port           string `yaml:"port"`
		SenderEmail    string `yaml:"sender_email"`
		SenderPassword string `yaml:"sender_password"`
	} `yaml:"smtp"`
	ReceiverEmail string `yaml:"receiver_email"`
}

func loadConfig(path string) (*Config, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config Config
	err = yaml.Unmarshal(file, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func sendMail(config *Config, subject, body string) error {
	auth := smtp.PlainAuth("", config.SMTP.SenderEmail, config.SMTP.SenderPassword, config.SMTP.Host)

	msg := strings.Join([]string{
		"From: 路由器测速 <" + config.SMTP.SenderEmail + ">",
		"To: " + config.ReceiverEmail,
		"Subject: " + subject,
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}, "\r\n")

	addr := fmt.Sprintf("%s:%s", config.SMTP.Host, config.SMTP.Port)
	return smtp.SendMail(addr, auth, config.SMTP.SenderEmail, []string{config.ReceiverEmail}, []byte(msg))
}

func main() {
	fmt.Println("加载配置...")
	config, err := loadConfig("config.yaml")
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		return
	}

	fmt.Println("开始测速...")
	client := speedtest.New()

	user, err := client.FetchUserInfo()
	if err != nil {
		fmt.Printf("获取用户信息失败: %v\n", err)
	}

	servers, err := client.FetchServers()
	if err != nil {
		errMsg := fmt.Sprintf("获取测速节点失败: %v", err)
		fmt.Println(errMsg)
		_ = sendMail(config, "路由器测速【失败】", errMsg)
		return
	}

	targets, err := servers.FindServer([]int{})
	if err != nil || len(targets) == 0 {
		errMsg := fmt.Sprintf("无可用测速节点: %v", err)
		fmt.Println(errMsg)
		_ = sendMail(config, "路由器测速【失败】", errMsg)
		return
	}
	target := targets[0]

	if err = target.PingTest(nil); err != nil {
		panic(err)
	}
	if err = target.DownloadTest(); err != nil {
		panic(err)
	}
	if err = target.UploadTest(); err != nil {
		panic(err)
	}

	now := time.Now().Format("2006-01-02 15:04:05")

	body := fmt.Sprintf(`===== 路由器测速报告 =====
测试时间：%s
公网IP：%s
运营商：%s
测速节点：%s (距离 %.2f km)
空载延迟：%.2f ms
下载速度：%.2f Mbps
上传速度：%.2f Mbps
`,
		now,
		user.IP,
		user.Isp,
		target.Name,
		target.Distance,
		target.Latency.Seconds()*1000,
		target.DLSpeed,
		target.ULSpeed,
	)

	fmt.Println(body)

	err = sendMail(config, "路由器测速【成功】", body)
	if err != nil {
		fmt.Printf("邮件发送失败: %v\n", err)
		return
	}
	fmt.Println("测速完成，邮件已发送！")
}
