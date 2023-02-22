package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/qingconglaixueit/wechatbot/pkg/logger"
	"github.com/qingconglaixueit/wechatbot/service"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var _ MessageHandlerInterface = (*GroupMessageHandler)(nil)

// GroupMessageHandler 群消息处理
type GroupMessageHandler struct {
	// 获取自己
	self *openwechat.Self
	// 群
	group *openwechat.Group
	// 接收到消息
	msg *openwechat.Message
	// 发送的用户
	sender *openwechat.User
	// 实现的用户业务
	service service.UserServiceInterface
}

func GroupMessageContextHandler() func(ctx *openwechat.MessageContext) {
	return func(ctx *openwechat.MessageContext) {
		msg := ctx.Message
		// 获取用户消息处理器
		handler, err := NewGroupMessageHandler(msg)
		if err != nil {
			logger.Warning(fmt.Sprintf("init group message handler error: %v", err))
			return
		}

		// 处理用户消息
		err = handler.handle()
		if err != nil {
			logger.Warning(fmt.Sprintf("handle group message error: %v", err))
		}
	}
}

// NewGroupMessageHandler 创建群消息处理器
func NewGroupMessageHandler(msg *openwechat.Message) (MessageHandlerInterface, error) {
	sender, err := msg.Sender()
	if err != nil {
		return nil, err
	}
	group := &openwechat.Group{User: sender}
	groupSender, err := msg.SenderInGroup()
	if err != nil {
		return nil, err
	}

	userService := service.NewUserService(c, groupSender)
	handler := &GroupMessageHandler{
		self:    sender.Self,
		msg:     msg,
		group:   group,
		sender:  groupSender,
		service: userService,
	}
	return handler, nil

}

// handle 处理消息
func (g *GroupMessageHandler) handle() error {
	if g.msg.IsText() {
		return g.ReplyText()
	}
	return nil
}

// ReplyText 发息送文本消到群
func (g *GroupMessageHandler) ReplyText() error {
	if time.Now().Unix()-g.msg.CreateTime > 60 {
		return nil
	}

	maxInt := rand.New(rand.NewSource(time.Now().UnixNano())).Intn(5)
	time.Sleep(time.Duration(maxInt+1) * time.Second)

	log.Printf("Received Group[%v], Content[%v], CreateTime[%v]", g.group.NickName, g.msg.Content,
		time.Unix(g.msg.CreateTime, 0).Format("2006/01/02 15:04:05"))

	var (
		err   error
		reply string
	)

	// 1.不是@的不处理
	if !g.msg.IsAt() {
		return nil
	}

	// 2.获取请求的文本，如果为空字符串不处理
	requestText := g.getRequestText()
	if requestText == "" {
		log.Println("group message is empty")
		return nil
	}

	requestText = strings.TrimSpace(requestText)
	requestText = strings.Trim(requestText, "\n")
	requestText = strings.Trim(requestText, "\n\n")
	requestText = strings.Replace(requestText, "\n", "", -1)


	log.Println("GPTPlus requestText:" + requestText)
	buffer := searchByKeyWords(requestText)

	reply = buffer.String()
	log.Println("GPTPlus 返回内容:" + reply)

	//if err != nil {
	//	text := err.Error()
	//	if strings.Contains(err.Error(), "context deadline exceeded") {
	//		text = deadlineExceededText
	//	}
	//	_, err = g.msg.ReplyText(text)
	//	if err != nil {
	//		return fmt.Errorf("reply group error: %v", err)
	//	}
	//	return err
	//}

	// 4.设置上下文，并响应信息给用户
	g.service.SetUserSessionContext(requestText, reply)
	_, err = g.msg.ReplyText(g.buildReplyText(reply))
	if err != nil {
		return fmt.Errorf("reply group error: %v ", err)
	}

	// 5.返回错误信息
	return err
}

// getRequestText 获取请求接口的文本，要做一些清洗
func (g *GroupMessageHandler) getRequestText() string {
	// 1.去除空格以及换行
	requestText := strings.TrimSpace(g.msg.Content)
	requestText = strings.Trim(g.msg.Content, "\n")

	// 2.替换掉当前用户名称
	replaceText := "@" + g.self.NickName
	requestText = strings.TrimSpace(strings.ReplaceAll(g.msg.Content, replaceText, ""))
	if requestText == "" {
		return ""
	}

	// 3.获取上下文拼接在一起,如果字符长度超出4000截取为4000(GPT按字符长度算),达芬奇3最大为4068,也许后续为了适应要动态进行判断
	sessionText := g.service.GetUserSessionContext()
	if sessionText != "" {
		 requestText = sessionText + "  " + requestText
	}
	if len(requestText) >= 4000 {
		requestText = requestText[:4000]
	}

	// 4.检查用户发送文本是否包含结束标点符号
	punctuation := ",.;!?，。！？、…"
	runeRequestText := []rune(requestText)
	lastChar := string(runeRequestText[len(runeRequestText)-1:])
	if strings.Index(punctuation, lastChar) < 0 {
		requestText = requestText + "？" // 判断最后字符是否加了标点,没有的话加上句号,避免openai自动补齐引起混乱
	}

	// 5.返回请求文本
	return requestText
}

// buildReply 构建回复文本
func (g *GroupMessageHandler) buildReplyText(reply string) string {
	// 1.获取@我的用户
	atText := "@" + g.sender.NickName
	textSplit := strings.Split(reply, "\n\n")
	if len(textSplit) > 1 {
		trimText := textSplit[0]
		reply = strings.Trim(reply, trimText)
	}
	reply = strings.TrimSpace(reply)
	if reply == "" {
		return atText + " " + deadlineExceededText
	}

	// 2.拼接回复, @我的用户, 问题, 回复
	replaceText := "@" + g.self.NickName
	question := strings.TrimSpace(strings.ReplaceAll(g.msg.Content, replaceText, ""))
	hr := strings.Repeat("-", 36)
	reply = atText + "\n" + question + "\n" + hr + "\n" + reply
	reply = strings.Trim(reply, "\n")
	reply = strings.Trim(reply, "\n\n")

	// 3.返回回复的内容
	return reply
}

/**
通过关键字搜索plus版本返回内容
 */
func searchByKeyWords(q string) bytes.Buffer {

	var buffer bytes.Buffer

	client := &http.Client{}

	fullQ := `{"action":"next","messages":[{"id":"1c06429f-7cfa-4e92-b785-ea978cf77871","author":{"role":"user"},"role":"user","content":{"content_type":"text","parts":["`+q+`"]}}],"parent_message_id":"b8acf723-ccc7-43c6-bec3-5fd3effb9542","model":"text-davinci-002-render-paid"}`
	log.Println("fullQ:" + fullQ)
	var data = strings.NewReader(fullQ)
	req, err := http.NewRequest("POST", "https://chat.openai.com/backend-api/conversation", data)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("authority", "chat.openai.com")
	req.Header.Set("accept", "text/event-stream")
	req.Header.Set("accept-language", "en,lb;q=0.9,gd;q=0.8,zh-CN;q=0.7,zh;q=0.6")
	req.Header.Set("authorization", "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6Ik1UaEVOVUpHTkVNMVFURTRNMEZCTWpkQ05UZzVNRFUxUlRVd1FVSkRNRU13UmtGRVFrRXpSZyJ9.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL3Byb2ZpbGUiOnsiZW1haWwiOiJzdHJvbmdhbnQxOTk0QGdtYWlsLmNvbSIsImVtYWlsX3ZlcmlmaWVkIjp0cnVlLCJnZW9pcF9jb3VudHJ5IjoiVEgifSwiaHR0cHM6Ly9hcGkub3BlbmFpLmNvbS9hdXRoIjp7InVzZXJfaWQiOiJ1c2VyLVZoaTI4czZiVkNValc1Vkx3VWdTZnZoNiJ9LCJpc3MiOiJodHRwczovL2F1dGgwLm9wZW5haS5jb20vIiwic3ViIjoiYXV0aDB8NjM4ZWE4ZGEzZTExZmUwMDhmM2Q4NWY0IiwiYXVkIjpbImh0dHBzOi8vYXBpLm9wZW5haS5jb20vdjEiLCJodHRwczovL29wZW5haS5vcGVuYWkuYXV0aDBhcHAuY29tL3VzZXJpbmZvIl0sImlhdCI6MTY3NjEzMjQ5MCwiZXhwIjoxNjc3MzQyMDkwLCJhenAiOiJUZEpJY2JlMTZXb1RIdE45NW55eXdoNUU0eU9vNkl0RyIsInNjb3BlIjoib3BlbmlkIHByb2ZpbGUgZW1haWwgbW9kZWwucmVhZCBtb2RlbC5yZXF1ZXN0IG9yZ2FuaXphdGlvbi5yZWFkIG9mZmxpbmVfYWNjZXNzIn0.jMNKKjor4U05KpUfdeQ3odnFexb2qfYJ4gzNrdfkRm3pk8pyd55NKDe2kwNiX58bGWzSj84UIx2NbJ8FsgqfQaEYrIyCO8G-rwMDGF5GeZaALZ_rt5VcyZmJR9AYoR3iAwv5EckrSkJiQxJfA0SJ7cgGSeXmXuJ9yfnXhFzX7LVTYk1WYsp_imXmJNboXSno13GH8lQhtRdykWeObhUWM7pB2LqzvdhqOXaFvmNFg4eiKr6YWoa6dNGTtIB6uboCScE8nO1UPp7RzKetsu-dm-JjPpJ1XgoXMUexUhrewkR4NlLOVEwTDgYyAQ8xVl5KvFkWUQzXzll-aYuKoY7c0A")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cookie", "_ga=GA1.2.669054835.1671078900; intercom-device-id-dgkjq2bp=7087b55f-5748-44f0-9309-f556e74bbbbc; cf_clearance=drddXGMD3PdQp6wn4JeFCEkt96R2Dg3stjjKM4yakH0-1675955023-0-1-b8a7de08.74352235.efa681e8-160; cf_clearance=oe2Y50j3d8IJ23aZjkPmxK68dOUnfDsS4MR_URgKMC8-1676130288-0-1-b8a7de08.917f4928.efa681e8-160; __Host-next-auth.csrf-token=41a3b7e4593958d711423da7847e5f227341841d2b7ea9317b047202bfe2db71%7Cd464dd81a662df019a95f32d1b4c77e7bbaf81da6795f2abf93f084d838ef13e; __Secure-next-auth.callback-url=https%3A%2F%2Fchat.openai.com; mp_d7d7628de9d5e6160010b84db960a7ee_mixpanel=%7B%22distinct_id%22%3A%20%22user-i0rb6ZwJlkUnrViWruiaxjsg%22%2C%22%24device_id%22%3A%20%2218636b3d153d3d-00eb6bcc5950d2-16525635-fa000-18636b3d154ca7%22%2C%22%24initial_referrer%22%3A%20%22https%3A%2F%2Fplatform.openai.com%2F%22%2C%22%24initial_referring_domain%22%3A%20%22platform.openai.com%22%2C%22%24search_engine%22%3A%20%22google%22%2C%22%24user_id%22%3A%20%22user-i0rb6ZwJlkUnrViWruiaxjsg%22%7D; _cfuvid=n2wZlzICU8T7YXVwetuAs8DLEnbMgX0WH8AIaLxd5Vw-1677080939895-0-604800000; __Secure-next-auth.session-token=eyJhbGciOiJkaXIiLCJlbmMiOiJBMjU2R0NNIn0..f7igd1Hgxtr_fXQ1.h3Q1cHpAdkWPGfHeCrwqfVgdWBrd3V8hQt4edHKIX6bHxN6IZ0u72s2lTsI-JN6neZSaKKI8LJEGn4AX8tZ9LbHbJFTaZWxHpHLJbqdH7fWViNBrwHnkqqNiuXi1HpeTx4q0xKggmIkLdeg3YZZXKGu_UgI2eqsVjumE8LK8FBnyNYsd5sS784BkRkIFZcDZQYkNBptU_GO8h0ItBpdrKUklSyvZKMkm3MZO9zSJdMWhxPISi1CaJB7XhoLTXu2n3mM_NglUeMAgXu6cUsfnZWKW3VN0fdjEIcr0B0mk9xrN7TH1z0lV1Tr3Wuv6c_13EPDlxhMpb3ORcpkT2k1X19rEXVxm2q5riZn9_Mn3xdWJf3DUPRZ7_CDPc2fcXwrsY0gly0aUBS1m7rFH9Rm8aZhPgqUrtm3822wQ_5KQXR6VdvNXhzlnAqk9aExBLyqH0z8g2by8dYDbqw2rUq0CYLSrRZYhs2l7HvE4VYYjYuhpnoI-cjtrHofXsP-QsLL0Z7KkIN-IpjPowv9SAJqmuDpiDkTVvGYzfdAkLJvUDkyL0yG3fnbJgP_a4PiH2W2fOe7JTBr4szBJ_4Rn7WaNeQG6_R_OdrC4sMIUkzVSU2E10EY5aRORq87tfeycw5Q3emVW8gkhe4wl-8zxjV2ByaaldhDVrfRQAKct-KKg4onK4GraixDbAYkSexqhcMzqxysuGtvfgDG3SOmUA6DFJ7Nw51o_-sZgDfeV6WsTn3AXvtohyUN7-VLnXpGy7Ck4D07ZzALSeDssCspnfOBC-pYeFJvAXGk_PXAFaddlzfzXiY-ZB5k9kJjvRhUKbk9_PzV0lVQ-2weebLzCEu7TlFh4HZhgIIcii5-HwWkkjcbDixrCgGyx4Fic-aDHRnjLOwRBTndT4lnUyR7ycdP-TueB3153sp6HCLIE_S0eoqtYEBAoc3M9PLXN4Ki57s37zGAbzTx_0NPo-94Owa15amXuAvq3iNUros1sDRLuVK61AW8wh8zv07aH-5fkWbek8Z4LneMeBAHbPhSRI007-ohc2dvOV1Vh9q3WUK_ECqk0nKnnKdK4Wie0EgyJpAMArs2CCB6Vz80AL7rKJ3k_i9zJePqVDAq5vR1iwW_Nn_IFPAI3n_RgFCX9pzCdvBnfNagmfaSpx_kM_qOo5HWX7k_7JzxacAHXGHe8IF0fOSndNQQHLnYDDRR1CIjx9gcWS7-bNl7d4b8Zf-qIFUR49L5NdCT0bQ2jlofdv3TdrMQfD7LFsmfdkZUCxB0FhxFFufVCnGCHrd2Cxc_Cm63oRuz5XU8bYl7f-VcwkYelkLoIV_aQkIOZIJ8xgJaDe5a96h2QGFMjGacNAUqa_iZChpFmb7yvzNK1SepTajCxLZ08QpiKgHwL74Rs0IHerc-THWuFT1JhOfX-v6RXWVHQO95FCut2DOxFGnEoXCczDsjDFOcCSH8tvkJ4RreS6S8-wL4j0QKBKNzQ0uKhcgyX-7MotH3jx369LnWK4ZzHMouDAkCixBXpLDDQjEix9FvYtn63k_lJayBQull1-TVKSiDMYC51B4UfJX6918L1ajqb9LoMp03S6Ej3kPkjHeu9RkBQq6I6e58v0pdVD2NMCcipJZRSvBEMH5bYd6FHWajIb0BtPGfpAIn1BoN1sd3L1HVkBNRaNZdvSWRdmMhZgMFJC17k8uWJKLoEem0uxT9yRCHkXSjSfEjHrIjr91NV43hNcrvEJTKUMFA2zAbvlmwBOQwTofLOGDH056cws8pOuBPI873uICUgsBvYSKCpExyjCT0frz2x8CcSoVRS64Mg_8OJyGkqQsjYfq2ByDRR5-tMDP8pwDZYqJgKD4HDdNw7BK0MvOLXS0Ga_MDx5HcV2Q1EjI3Ba-xrQ95TZtpPtocwbJ88x8hqLBiuOGfs1Re8owWqqtDXhaL0cvTuwthxvLtHpHjbODeGkj7SkAF6BXUQ1J3HmHl6r2J0HqNQbohk81Qdhjs_w6C2LnHPB-w_AiA982uxweQzl1o7ZDy1OwXS186yo8W9WZpAMZyuU9pr_5Sb049dlIJm5SzpGihoR0mVPIKqeFpPE0GaZrNjhqwNmt32p2YTOAdOCTYkdEdZ9_aktq6vB6z5hoOgt0D7bmtdQuagdGbwYhGHdlra0KbiW2vyJ0v67R2vDdKNhx88Hp9J6ddb1WICvogQOf0srG0U_UU68Jr2z8NFjAcGCDvFLgezThnntMIqUYOgtZbkXmu0vdGL65hlAh8B9hzHwFpr4AYI_Sm8qg5tWqfo_FfN6Dh-qoFd9wvJ6auG-_bRzjYqS2V_Zf_XhmxLuwIX81g9JmddE_2hzgUfPaCsjS3UPg2b42A1PNrKczVuOq3br6REmCy2NUfiKMH2et9C-VqJMf98RWFIJDj2eV7cpbz8vPfEacej3SHs7UkOe7-rLrkuNW5Ydk_walYiMwFQ2vcwfhtONEZw_d7mNFVG5RZiWHhfUHvjQTpvkywUVyuQAWSadGOM-w1OHBTo_gYUo4NPXIktIouFcHFGun6YKzyfFrDwHprW9E3xelcea3Qxr36Vhfkic8LOEIx9arOCT8S0ZM1FVrFCrGML2u0jEcg5hkgloTJjlRIPiih0nxtO_ITUcl2Td00mKam5xk8wOWsm8nfU0PE7ZELBs2p8sHHcdpQMX9G9rtA.jtJVPBpUS9L56F-kmNyG4A; _puid=user-Vhi28s6bVCUjW5VLwUgSfvh6:1677082143-%2FEbyhCdEn9NwXHcsTzMbIq6ZdXrcHNvzfchiX36AZbg%3D; __cf_bm=tE5g3VLoJ0SQwt.vw1dL9kh6K0yRvmvI21e42EljGVU-1677082144-0-AVyLXi5gIbk6FVV2YXkBMO8TIBWsP9tqUZE0IrLqnXFcslKBuLVHaSqLn6BDz7FkKOH4439FHV9S56iThmh6WWOwHYm4TvWNGBlLBbTLq/G/kGaxf8mlNygZpTpCzbpAECxyUNHlP987qJGSIfDbG3lRMWlD2Cw4TN/OscO8Fg5yWBAGBqCjQGZs7A2LBdLnEQ==")
	req.Header.Set("origin", "https://chat.openai.com")
	req.Header.Set("referer", "https://chat.openai.com/chat?model=text-davinci-002-render-paid")
	req.Header.Set("sec-ch-ua", `"Not_A Brand";v="99", "Google Chrome";v="109", "Chromium";v="109"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/109.0.0.0 Safari/537.36")
	resp, err := client.Do(req)

	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	content := string(bodyText)
	log.Println("resp:" + content)

	content = strings.ReplaceAll(content, "[DONE]", "")

	var r = strings.Split(content, "data:")

	for index, line := range r {
		if index == len(r)-2 {

			// Declared an empty map interface
			var result map[string]interface{}

			// Unmarshal or Decode the JSON to the interface.
			json.Unmarshal([]byte(line), &result)

			parts := result["message"].(map[string]interface{})["content"].(map[string]interface{})["parts"].([]interface{})
			for _, part := range parts {
				str := fmt.Sprintf("%v", part)
				buffer.WriteString(str)
			}
		}
	}
	return buffer
}

func zhToUnicode(raw []byte) ([]byte, error) {
	str, err := strconv.Unquote(strings.Replace(strconv.Quote(string(raw)), `\\u`, `\u`, -1))
	if err != nil {
		return nil, err
	}
	return []byte(str), nil
}

