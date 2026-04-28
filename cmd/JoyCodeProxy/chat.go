package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vibe-coding-labs/JoyCodeProxy/pkg/joycode"
)

var (
	chatModel     string
	chatStream    bool
	chatMaxTokens int
)

var chatCmd = &cobra.Command{
	Use:     "chat [message]",
	Short:   "发送聊天消息",
	Long:    "通过 JoyCode API 发送一条聊天消息并返回响应。",
	GroupID: "core",
	Example: `  # 发送简单消息
  joycode-proxy chat "你好"

  # 指定模型
  joycode-proxy chat -m GLM-5.1 "写一个排序算法"

  # 流式输出
  joycode-proxy chat -s "解释量子计算"`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := resolveClient()
		if err != nil {
			return err
		}
		body := map[string]interface{}{
			"model":      chatModel,
			"messages":   []map[string]interface{}{{"role": "user", "content": args[0]}},
			"stream":     false,
			"max_tokens": chatMaxTokens,
		}
		if chatStream {
			body["stream"] = true
			return streamChat(client, body)
		}
		resp, err := client.Post("/api/saas/openai/v1/chat/completions", body)
		if err != nil {
			return err
		}
		choices, _ := resp["choices"].([]interface{})
		if len(choices) > 0 {
			choice, _ := choices[0].(map[string]interface{})
			msg, _ := choice["message"].(map[string]interface{})
			content, _ := msg["content"].(string)
			fmt.Println(content)
		}
		return nil
	},
}

func streamChat(client *joycode.Client, body map[string]interface{}) error {
	resp, err := client.PostStream("/api/saas/openai/v1/chat/completions", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			fmt.Print(string(buf[:n]))
		}
		if readErr != nil {
			break
		}
	}
	fmt.Println()
	return nil
}

func init() {
	chatCmd.Flags().StringVarP(&chatModel, "model", "m", "JoyAI-Code", "模型名称")
	chatCmd.Flags().BoolVarP(&chatStream, "stream", "s", false, "流式输出")
	chatCmd.Flags().IntVar(&chatMaxTokens, "max-tokens", 64000, "最大输出 token 数")
	rootCmd.AddCommand(chatCmd)
}
