package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/rivo/tview"

	"github.com/dundee/gdu/v5/pkg/fs"
)

const maxAIContextFiles = 20

func (ui *UI) showAIAnalysis(item fs.Item) {
	if ui.aiApiKey == "" {
		ui.showErr("AI API key is not set. Please set GDU_AI_API_KEY environment variable", nil)
		return
	}
	if ui.aiBaseURL == "" {
		ui.showErr("AI API base URL is not set. Please set GDU_AI_BASE_URL environment variable", nil)
		return
	}

	text := tview.NewTextView().
		SetText("AI is analyzing...").
		SetTextAlign(tview.AlignCenter)
	text.SetBorder(true).SetTitle(" AI Analysis ")
	flex := modal(text, 60, 5)
	ui.pages.AddPage("ai-loading", flex, true, true)

	go func() {
		prompt := ui.buildAIPrompt(item)
		result, err := ui.callAI(prompt)

		ui.app.QueueUpdateDraw(func() {
			ui.pages.RemovePage("ai-loading")
			if err != nil {
				ui.showErr("AI analysis failed", err)
				return
			}
			ui.showAIResult(result)
		})
	}()
}

func (ui *UI) buildAIPrompt(item fs.Item) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("路径: %s\n", item.GetPath()))
	b.WriteString(fmt.Sprintf("名称: %s\n", item.GetName()))
	b.WriteString(fmt.Sprintf("类型: %s\n", map[bool]string{true: "目录", false: "文件"}[item.IsDir()]))
	b.WriteString(fmt.Sprintf("大小: %s\n", ui.formatSize(item.GetUsage(), true, false)))

	if item.IsDir() {
		b.WriteString("\n包含的前20个文件/子目录:\n")
		count := 0
		for child := range item.GetFiles(fs.SortByName, fs.SortAsc) {
			if count >= maxAIContextFiles {
				break
			}
			prefix := "  [文件]"
			if child.IsDir() {
				prefix = "  [目录]"
			}
			b.WriteString(fmt.Sprintf("%s %s\n", prefix, child.GetName()))
			count++
		}
		if count == 0 {
			b.WriteString("  (空目录)\n")
		}
	}

	b.WriteString("\n请分析这个路径的用途，并判断是否可以安全删除。如果删除有风险，请说明原因。回答请使用中文，可以使用 Markdown 格式（如标题、列表、粗体等）以便更好地组织内容。")

	return b.String()
}

func (ui *UI) callAI(prompt string) (string, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}

	type requestBody struct {
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}

	type choice struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}

	type responseBody struct {
		Choices []choice `json:"choices"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	reqData := requestBody{
		Model: ui.aiModel,
		Messages: []message{
			{Role: "user", Content: prompt},
		},
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	req, err := http.NewRequest(http.MethodPost, ui.aiBaseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+ui.aiApiKey)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	var respData responseBody
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}

	if respData.Error != nil {
		return "", fmt.Errorf("API error: %s", respData.Error.Message)
	}

	if len(respData.Choices) == 0 {
		return "", fmt.Errorf("no choices in API response")
	}

	return respData.Choices[0].Message.Content, nil
}

func renderMarkdownToPlain(input string) (string, error) {
	r, err := glamour.NewTermRenderer(glamour.WithWordWrap(76))
	if err != nil {
		return "", fmt.Errorf("creating glamour renderer: %w", err)
	}
	out, err := r.Render(input)
	if err != nil {
		return "", fmt.Errorf("rendering markdown: %w", err)
	}
	ansiEscape := regexp.MustCompile(`\x1b(?:[@-Z\-_]|\[[0-?]*[ -/]*[@-~])`)
	return ansiEscape.ReplaceAllString(out, ""), nil
}

func (ui *UI) showAIResult(result string) {
	plain, err := renderMarkdownToPlain(result)
	if err != nil {
		ui.showErr("Failed to render AI result", err)
		return
	}

	text := tview.NewTextView().
		SetText(plain).
		SetScrollable(true)
	text.SetBorder(true).SetTitle(" AI Analysis Result ")

	// Calculate a reasonable height based on screen size
	_, screenHeight := ui.screen.Size()
	height := screenHeight - 10
	if height < 10 {
		height = 10
	}

	flex := modal(text, 80, height)
	ui.pages.AddPage("ai-result", flex, true, true)
	ui.app.SetFocus(text)
}
