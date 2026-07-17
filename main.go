package main

import (
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/showwin/speedtest-go/speedtest"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Email struct {
		Enabled    bool   `yaml:"enabled"`
		From       string `yaml:"from"`
		To         string `yaml:"to"`
		AuthCode   string `yaml:"auth_code"`
		SMTPServer string `yaml:"smtp_server"`
		SMTPPort   int    `yaml:"smtp_port"`
	} `yaml:"email"`
	Timeout int `yaml:"timeout"`
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
	if !config.Email.Enabled {
		return nil
	}

	auth := smtp.PlainAuth("", config.Email.From, config.Email.AuthCode, config.Email.SMTPServer)

	msg := strings.Join([]string{
		"From: 路由器测速 <" + config.Email.From + ">",
		"To: " + config.Email.To,
		"Subject: " + subject,
		"Content-Type: text/plain; charset=utf-8",
		"",
		body,
	}, "\r\n")

	addr := fmt.Sprintf("%s:%d", config.Email.SMTPServer, config.Email.SMTPPort)

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, config.Email.SMTPServer)
	if err != nil {
		return err
	}

	if err = client.Auth(auth); err != nil {
		return err
	}

	if err = client.Mail(config.Email.From); err != nil {
		return err
	}

	if err = client.Rcpt(config.Email.To); err != nil {
		return err
	}

	w, err := client.Data()
	if err != nil {
		return err
	}

	_, err = w.Write([]byte(msg))
	if err != nil {
		return err
	}

	err = w.Close()
	if err != nil {
		return err
	}

	return client.Quit()
}

func main() {
	fmt.Println("加载配置...")
	config, err := loadConfig("config.yaml")
	if err != nil {
		fmt.Printf("加载配置失败: %v\n", err)
		return
	}

	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	fmt.Printf("设置超时时间: %d 秒\n", timeout)

	fmt.Println("开始测速...")

	http.DefaultClient = &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: time.Duration(timeout) * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: time.Duration(timeout) * time.Second,
		},
	}

	client := speedtest.New()

	fmt.Println("正在获取用户信息...")
	user, err := client.FetchUserInfo()
	if err != nil {
		fmt.Printf("获取用户信息失败: %v\n", err)
	} else {
		fmt.Printf("用户信息: IP=%s, 运营商=%s, 位置=%s\n", user.IP, user.Isp, user.Location)
	}

	fmt.Println("正在获取测速节点列表...")
	servers, err := client.FetchServers()
	if err != nil {
		errMsg := fmt.Sprintf("获取测速节点失败: %v", err)
		fmt.Println(errMsg)
		_ = sendMail(config, "路由器测速【失败】", errMsg)
		return
	}
	fmt.Printf("共获取到 %d 个测速节点\n", len(servers))

	fmt.Println("正在筛选最优节点...")
	targets, err := servers.FindServer([]int{})
	if err != nil || len(targets) == 0 {
		errMsg := fmt.Sprintf("无可用测速节点: %v", err)
		fmt.Println(errMsg)
		_ = sendMail(config, "路由器测速【失败】", errMsg)
		return
	}

	fmt.Printf("发现 %d 个可用测速节点\n", len(targets))
	for i, s := range targets {
		if i >= 5 {
			break
		}
		fmt.Printf("  节点 %d: %s (ID:%d, 距离:%.2f km, 主机:%s)\n", i+1, s.Name, s.ID, s.Distance, s.Host)
	}

	var target *speedtest.Server
	var lastErr error
	maxRetries := 3

	for i := 0; i < len(targets) && i < maxRetries; i++ {
		candidate := targets[i]
		fmt.Printf("\n尝试测速节点 %d/%d:\n", i+1, maxRetries)
		fmt.Printf("  名称: %s\n", candidate.Name)
		fmt.Printf("  ID: %d\n", candidate.ID)
		fmt.Printf("  距离: %.2f km\n", candidate.Distance)
		fmt.Printf("  主机: %s\n", candidate.Host)
		fmt.Printf("  IP: %s\n", candidate.IP)

		fmt.Println("  正在进行Ping测试...")
		start := time.Now()
		if err = candidate.PingTest(nil); err != nil {
			elapsed := time.Since(start)
			lastErr = fmt.Errorf("Ping测试失败: %v", err)
			fmt.Printf("  Ping测试失败: %v (耗时: %v)\n", err, elapsed)
			continue
		}
		elapsed := time.Since(start)
		fmt.Printf("  Ping测试成功: 延迟 %.2f ms (耗时: %v)\n", candidate.Latency.Seconds()*1000, elapsed)

		fmt.Println("  正在进行下载测试...")
		start = time.Now()
		if err = candidate.DownloadTest(); err != nil {
			elapsed := time.Since(start)
			lastErr = fmt.Errorf("下载测试失败: %v", err)
			fmt.Printf("  下载测试失败: %v (耗时: %v)\n", err, elapsed)
			continue
		}
		elapsed = time.Since(start)
		fmt.Printf("  下载测试成功: %.2f Mbps (耗时: %v)\n", candidate.DLSpeed, elapsed)

		fmt.Println("  正在进行上传测试...")
		start = time.Now()
		if err = candidate.UploadTest(); err != nil {
			elapsed := time.Since(start)
			lastErr = fmt.Errorf("上传测试失败: %v", err)
			fmt.Printf("  上传测试失败: %v (耗时: %v)\n", err, elapsed)
			continue
		}
		elapsed = time.Since(start)
		fmt.Printf("  上传测试成功: %.2f Mbps (耗时: %v)\n", candidate.ULSpeed, elapsed)

		target = candidate
		break
	}

	if target == nil {
		errMsg := fmt.Sprintf("所有节点测速失败: %v", lastErr)
		fmt.Println(errMsg)
		_ = sendMail(config, "路由器测速【失败】", errMsg)
		return
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