package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/eatmoreapple/openwechat"
	"github.com/google/uuid"
	"github.com/qingconglaixueit/wechatbot/config"
	"github.com/qingconglaixueit/wechatbot/pkg/logger"
	"github.com/qingconglaixueit/wechatbot/service"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
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

type Image struct {
	URL string `json:"url"`
}

type ImageData struct {
	Created int64    `json:"created"`
	Data    []Image `json:"data"`
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

	// 1.1.清空会话的不处理
	if strings.Contains(g.getRequestText(),config.LoadConfig().SessionClearToken) {
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

	if strings.Contains(requestText,"img") {
		bodyText := searchReturnImage(requestText, err)
		fmt.Printf("%s", bodyText)


		var imgData ImageData
		err := json.Unmarshal([]byte(bodyText), &imgData)
		if err != nil {
			fmt.Println(err)
			return nil
		}

		fmt.Println(imgData.Data[0].URL)

		// don't worry about errors
		response, e := http.Get(imgData.Data[0].URL)
		if e != nil {
			log.Fatal(e)
		}
		defer response.Body.Close()

		uid := uuid.New().String()

		//open a file for writing
		file, err := os.Create("/tmp/"+uid+".png")
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()





		// Use io.Copy to just dump the response body to the file. This supports huge files
		_, err = io.Copy(file, response.Body)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println("Success!")

		fi, err := os.Open("/tmp/"+uid+".png")
		if err != nil {
			log.Fatal(err)
		}
		defer fi.Close()

		name:= fi.Name()
		log.Println(name)

		_, err = g.msg.ReplyImage(fi)
		if err != nil {
			return fmt.Errorf("reply group error: %v ", err)
		}

		return nil
	}


	buffer := searchByKeyWords(requestText)

	reply = buffer.String()
	log.Println("GPTPlus 返回内容:" + reply)

	// 4.设置上下文，并响应信息给用户
	g.service.SetUserSessionContext(requestText, reply)
	_, err = g.msg.ReplyText(g.buildReplyText(reply))
	if err != nil {
		return fmt.Errorf("reply group error: %v ", err)
	}

	// 5.返回错误信息
	return err
}

func searchReturnImage(requestText string, err error) []byte {

	client := &http.Client{}
	var data = strings.NewReader(`{
    "prompt": "` + requestText + `",
    "n": 1,
    "size": "1024x1024"
  }`)
	req, err := http.NewRequest("POST", "https://api.openai.com/v1/images/generations", data)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer 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")
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	bodyText, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	return bodyText
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

	log.Println("search q:" + q)

	fullQ := `{"action":"next","messages":[{"id":"1b91162e-e040-4436-b9b7-e1f6912b3117","author":{"role":"user"},"role":"user","content":{"content_type":"text","parts":["`+q+`"]}}],"conversation_id":"f0b4e553-5a5e-481e-bc07-bd1726f99ffd","parent_message_id":"ef90cd10-4898-4592-8f94-da1ae8a05a5d","model":"text-davinci-002-render-paid"}`
	log.Println("fullQ:" + fullQ)
	var data = strings.NewReader(fullQ)
	req, err := http.NewRequest("POST", "https://chat.openai.com/backend-api/conversation", data)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("authority", "chat.openai.com")
	req.Header.Set("accept", "text/event-stream")
	req.Header.Set("accept-language", "en,lb;q=0.9,gd;q=0.8,zh-CN;q=0.7,zh;q=0.6")
	req.Header.Set("authorization", "Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCIsImtpZCI6Ik1UaEVOVUpHTkVNMVFURTRNMEZCTWpkQ05UZzVNRFUxUlRVd1FVSkRNRU13UmtGRVFrRXpSZyJ9.eyJodHRwczovL2FwaS5vcGVuYWkuY29tL3Byb2ZpbGUiOnsiZW1haWwiOiJzdHJvbmdhbnQxOTk0QGdtYWlsLmNvbSIsImVtYWlsX3ZlcmlmaWVkIjp0cnVlLCJnZW9pcF9jb3VudHJ5IjoiU0cifSwiaHR0cHM6Ly9hcGkub3BlbmFpLmNvbS9hdXRoIjp7InVzZXJfaWQiOiJ1c2VyLVZoaTI4czZiVkNValc1Vkx3VWdTZnZoNiJ9LCJpc3MiOiJodHRwczovL2F1dGgwLm9wZW5haS5jb20vIiwic3ViIjoiYXV0aDB8NjM4ZWE4ZGEzZTExZmUwMDhmM2Q4NWY0IiwiYXVkIjpbImh0dHBzOi8vYXBpLm9wZW5haS5jb20vdjEiLCJodHRwczovL29wZW5haS5vcGVuYWkuYXV0aDBhcHAuY29tL3VzZXJpbmZvIl0sImlhdCI6MTY3NzUwNDA2NiwiZXhwIjoxNjc4NzEzNjY2LCJhenAiOiJUZEpJY2JlMTZXb1RIdE45NW55eXdoNUU0eU9vNkl0RyIsInNjb3BlIjoib3BlbmlkIHByb2ZpbGUgZW1haWwgbW9kZWwucmVhZCBtb2RlbC5yZXF1ZXN0IG9yZ2FuaXphdGlvbi5yZWFkIG9mZmxpbmVfYWNjZXNzIn0.Vx4YcreOeDevNoIXNrdig-GDVHEeRF9eulhIWvN6cjJbejiLUf2o51YYNHgiiaWo2362B6JrUkp10mnYlfetMehrzEEKFg0jSyJS4ADJ2MsCceIaTdvBFRs63S71v8JB037uRfO4hBBtGRX4M5zc1amZfN65dvd0XQNrtGxL8yANvsFKAhySEB3e1aO5aVWdT8I9ksNC2a502SRk-z3CnxGbVwMn0Wa8z0VRtn6s_rIQWjvAjRbK_cb8cx32v4poSUuvOUMNrC4opcbeLG5TDW6RDl4TQT0QVg09xGRAYq0OId8yPL46m_iDVDft5Q9iJyiCb1zXB3xg4r3s-hxrjw")
	req.Header.Set("content-type", "application/json")
	req.Header.Set("cookie", "intercom-device-id-dgkjq2bp=7087b55f-5748-44f0-9309-f556e74bbbbc; cf_clearance=oe2Y50j3d8IJ23aZjkPmxK68dOUnfDsS4MR_URgKMC8-1676130288-0-1-b8a7de08.917f4928.efa681e8-160; mp_d7d7628de9d5e6160010b84db960a7ee_mixpanel=%7B%22distinct_id%22%3A%20%22user-i0rb6ZwJlkUnrViWruiaxjsg%22%2C%22%24device_id%22%3A%20%2218636b3d153d3d-00eb6bcc5950d2-16525635-fa000-18636b3d154ca7%22%2C%22%24initial_referrer%22%3A%20%22https%3A%2F%2Fplatform.openai.com%2F%22%2C%22%24initial_referring_domain%22%3A%20%22platform.openai.com%22%2C%22%24search_engine%22%3A%20%22google%22%2C%22%24user_id%22%3A%20%22user-i0rb6ZwJlkUnrViWruiaxjsg%22%7D; __Secure-next-auth.callback-url=https%3A%2F%2Fchat.openai.com; __Host-next-auth.csrf-token=ea020d6ca0e427c981204b139e311a723c0d0c58782e1bda2c37963c12587467%7C34632d104a2a524a49e6b57fb24265f159650da9607dc8a8e949562114e7fba8; _ga=GA1.2.669054835.1671078900; _gid=GA1.2.1478507419.1678106457; _ga_9YTZJE58M9=GS1.1.1678106434.1.1.1678106468.0.0.0; __cf_bm=fCRTK81I5iJgk9N7qAmC7tPuwC0id7GkgiZ0ttLmmP4-1678106815-0-AaobNzXm1K38Ac1kZOaNlUD92xv/hC6kIgnEsVi1do+nkMegOOuaQnDcwFzCxYWI+W4NRbZXlbvMAtfVFHKCJdfDzzckeVrMICk3FNapDn3ntupkNXWfJILMSpiZO69lDH1bUkkjV9Pk1k3AHJ9q83JTj9EIchqfqzA3csXQczZtxFxmkKy0ZsL1tBHndRPKkA==; cf_clearance=imZse0h13Ge.g3MCOyGnT0nu09.a5YCSSXhb188NslQ-1678106839-0-1-ddd5f387.680f0c7b.31e7c3d3-250; _cfuvid=tRxmEYbrp1OvqfD17XyGeA0x5qtatgQNw8NgQHuJLVs-1678108050207-0-604800000; __Secure-next-auth.session-token=eyJhbGciOiJkaXIiLCJlbmMiOiJBMjU2R0NNIn0..0HI7GHbPtFxrxaYa.3quyJMC1D9xtKZKAQukIgKhRrXiNgFcI47WTDahKhJIJXKfYyPZBbldtdr4-ay_bLKx6GtK1Utr3S2S93QOvKgw_i882D9zeq6hm8qC5FbEkFccJ6dwJjrjcpRdexTDfHPFNGawkkuxTZFvdpt0iEtkjAeTiE6tMa8WhXfHcjLuPaJFoONcj1I2r8qd1S2AkfbgcUcHw1vcnb6_unIhYFnQyd8f2Nj8_EK3R7j2hcfeeFKoafxcMRF98qz2phbAoSh_bT26cA357ZmgcjJMVncCClGjn3XmFiM9hwuR0Vam-50CgtCebeYXCJRb4cmfwpBbxBMex4FuJa465YyTyaqzGhppX72L3jW3x_InD__c12EEn0U_DbFNHKSyRooGIYVPYcH501n38VgiYXb5WWNQD1F7yJWROugCO7gvQI7zY_VNqe6wSPbbwSVlUzv3_DqrXcV8RswH5KsLh2iNSoo0B81J8ia83L9uewhAJ1AtKv4qIWbXPiqZKXxs0mbyMVjgpFfpYUU9UZ3n8Zid2hF5aKCxd3PmtYRS1l0ubhCl-fZUh6pioCxVyDeQh7DKAOs0Tll-WIBOAQljTWQVckRT6UD1xEXQBHKpJ59bsKVpCw-yPwAibYNbikb5e2nsF1nhrbJ9sQlW6fx_S6m_FFmy2uFjj7yipYL1uVEfii43WD3lxysIjrLQ_dxqza5jDKOBSBe3vDOUrWhdEtmgjAWQ3BdoJvRzWK2-P2XPb-DYMZ8B0aeaUHMJfoJTWboBj3zuep28jcWdA4ENeXAKvd0YTV0xwdTwpzWrxO7eCHLiT5qTP-PXgG1tYJSzjV1fXiAsywJ0U7Eu9qhwEQXoTd10GNcEZmHtCzkzUFN5ujyeZy7wtanQq6AvqB7vVnJLcqGH0qjuwZTIHWmy7GAudHHIO3CtlQgDWlBxJSQKo_8c8LuCTdWaLDuiVecsieZ_XikpBcb697AfSU28kv0PiRBQhBI_l7ClGzO7i0nhI-vzm0dKVjlYA9b6ewoxfxMFgNy3RVu5hwWtJDQGUYa8N78Gj0OGl6ybG3xSnrpivY6IlYjGegx6EOt8nw_jCNTZNclpQBdR_LraBfJCy-KN5QO8u7LjWMIWm7tfxRbIjs_RBPljcgxXjLlgxExqLixvvQ7DxeFZRSY_F7vtB_w_-akk1ZqJastdQPeEASOs9u7h7I8eC0oLIhDftnREdWVn31S6wn_H2MaEz2vM81O2abmw-fqfZjGagMd1ZwmHHVX1deNX5-EBKTIxm_yHIICnnpjk7mLAMio9Vrt7L87HSDqYq2Ntjdt_tAKKq0cgkBZ8jn6lyF0jE8za1QA6s_FSyN_acyw-cTM0-nHT8-4iYpiXOw5BcH5VgkiVIPjhGCccxgSM-OstZH3cZpnkhGMsfbzXhQlsoKiR-3ASEw9mtOp_cRjpXQICRiUbX1QbERus3vveTiv0PqKHaO78frDm1NODVHwNG4hYAKDxthgSupnM36ARNWTBqjxJGJtoUNoXkb66oUnWjNli1apQUxwqC1YS_DBXjrDalFIix7p6_BozJ77PghnLdj59gcu1gqtnQXRBKPvltm6vc634VwMObL3nAhc4-5BQqc24cq8px3eg5lGl78UfK30XScO7IhAw90WvHTtv7jw3SUFg_LdKHwb3yv_2U-oLPZQybBXq_y70uqTYfKpOzUmqiN9O1howcGKdZ-BEwvixobLwC5_eCyq0FquqBPIVQVqzZdSX7FEZkB1lSOqT7F1POZMKpz4Sul0-aUpE6KnqKaMSyO01JppkqvHojT_yQErjNnjgeV9Dsq4-5XQFiHWxJ5It86fO4KTXX3Cwfq785ONBMaENGniAXi-SHHMwdbxrPuDhhuvJ0ozlxVsDC9u8W2FWzoABfnFRS-bz-S9DFGimkIiBuGwOP8kMOfipLl8R9X6IPLH1h0mESfFjEECetMeAQRksF1YbgywDfqhKg3tVS4uyyv20B-DMxFYOmWrDJatSAUL6Z9a5a5q8BYhu-SOmlnvuoEFY1curXDvH3C8wnXN_Xq0nw1qGpNRpwusPXfzLauWj7itEp4PLws8pMd3t101WPjTqvy1Twe_KM9oFKFnUMGmFP1nHjyL1KVAQIR_-q86DjJOI71aZo7n3jLpDb5miv61IXHdGHjnGGoVlq4duLw7u8zSdFPN1tHU48fOnw86Hi8qKAglkIXpeovVTH4trvsvplYRWbVXEYr5forNKY6gLLZJk6cTg-QmHMQ5vlJ4x6V7ab90n1oZAXcXt5fs_zSDSOtKvYRepIC-OvDHIF4PGmEGiJfCbfM7IMQo4DpYoj2TMS1vjNMJUmcqdg5j13DiaTMdAU6tdYvQpwuD5KAqSuEBu1CpnQSNpowDpRLkKRgJ9OjKgVTeJDxAkjtqMducvJ6CWzenHpCWl1BEDLHsyxtvAKLzp3ODXB3B3nnDGOFz0tyDCt-dtGgfHyeMrqcBSiwdDfkBet5E3rc9ejsde_8manE7LiZ6hbP5gBGraEbhbRIkjzAMXOnRtHq5BoRK24iKl_5tTyC3rVYCcc9g9wcRdEFBCwzR0obhFsitxiVeQfO9MSglZc_OD4gM_axoFupiW129duIQy9UxrHWJ1dkWAkCT5Da7Ub7o5g3G0FHYH8zw48XWmRuijVJ_Q1R6rgLBaGzxKzCk0.FpnXI_GQ6kJVQ1DmGrbd1Q; _puid=user-Vhi28s6bVCUjW5VLwUgSfvh6:1678108059-qs3Gx07u7Y1%2FTr70NaCGsOm%2BCrw7OlYEyElRFgvTe5k%3D; __cf_bm=IjRnKs.jhsWPbM_8lg1EWQLdz_tbH4GNhP.d1MhpPIc-1678108093-0-AQe0h6E0z1rUY9LB9MylxOfYJ157I/PBKbTYpQ41f7AUS8FJ6h1ZnWfU/oVEpDQx6cUUn6RUt3nRxzo7IOMagTI=")
	req.Header.Set("origin", "https://chat.openai.com")
	req.Header.Set("referer", "https://chat.openai.com/chat?model=text-davinci-002-render-paid")
	req.Header.Set("sec-ch-ua", `"Chromium";v="110", "Not A(Brand";v="24", "Google Chrome";v="110"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"macOS"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/110.0.0.0 Safari/537.36")
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

