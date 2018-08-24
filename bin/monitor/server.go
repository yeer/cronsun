package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/yeer/cronsun"
	"github.com/yeer/cronsun/conf"
	"github.com/yeer/cronsun/db"
	"github.com/yeer/cronsun/event"
	"github.com/yeer/cronsun/log"
	"github.com/yeer/cronsun/node/cron"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"io/ioutil"
	slog "log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const ONE_SECOND = 1*time.Second + 10*time.Millisecond

var selectForJobLogList = bson.M{"command": 0, "output": 0}
var mgoDB *db.Mdb
var lastTime time.Time
var (
	level    = flag.Int("l", 0, "log level, -1:debug, 0:info, 1:warn, 2:error")
	confFile = flag.String("conf", "conf/files/base.json", "config file path")
)

const (
	f_datetime = "2006-01-02 15:04"
)

func StrToTime(st string) time.Time {
	t, _ := time.ParseInLocation(f_datetime, st, time.Local)
	return t
}

var timeout = time.Duration(5 * time.Second)

func dialTimeout(network, addr string) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}

type msgItem struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

type atItem struct {
	AtMobiles []string `json:"atMobiles"`
	IsAtAll   bool     `json:"isAtAll"`
}

type message struct {
	Msgtype  string  `json:"msgtype"`
	Markdown msgItem `json:"markdown"`
	At       atItem  `json:"at"`
}

func getLog() {
	if lastTime.IsZero() {
		lastTime = time.Now().Add(-time.Minute * 60)
	}
	fmt.Printf("%s", lastTime)
	query := bson.M{}
	begin := lastTime
	//StrToTime("2018-08-13 09:00:41.000Z")
	query["beginTime"] = bson.M{"$gt": begin}
	query["success"] = false
	//query["report"] = false
	sort := "beginTime=-1"
	//sort := "beginTime"

	var err error
	var data struct {
		Total int               `json:"total"`
		List  []*cronsun.JobLog `json:"list"`
	}
	data.List, data.Total, err = GetJobLogList(query, 1, 50, sort)
	if err != nil {
		log.Errorf("get list fail,status[%d], %s", http.StatusInternalServerError, err.Error())
		return
	}
	var ErrJob string
	for _, k := range data.List {
		ErrJob = fmt.Sprintf("%s- job_id:[*%s*](http://cronsun.julive.com/ui/#/log/%s)\t**%s**\t[%s]\t%s\n", ErrJob, k.JobId, k.Id.Hex(), k.Name, k.IP, k.BeginTime)
		lastTime = k.BeginTime
	}

	if ErrJob != "" {
		ErrJob = fmt.Sprintf("🤔 cronsun执行任务失败  \n%s", ErrJob)
		msg := message{
			Msgtype:  "markdown",
			Markdown: msgItem{Title: "cronsun执行任务失败", Text: ErrJob},
			//Markdown: {Title: "cronsun执行任务失败", Text: ErrJob},
			At: atItem{AtMobiles: []string{}, IsAtAll: false},
			//{Title: "cronsun执行报错", Text: ErrJob},
		}
		messa, _ := json.Marshal(msg)
		doRequest(messa)
	}
}

func doRequest(message []byte) {

	tr := &http.Transport{
		//使用带超时的连接函数
		Dial: dialTimeout,
		//建立连接后读超时
		ResponseHeaderTimeout: time.Second * 2,
	}
	client := &http.Client{
		Transport: tr,
		//总超时，包含连接读写
		Timeout: timeout,
	}

	request, err := http.NewRequest("POST", "https://oapi.dingtalk.com/robot/send?access_token=3c869d1b3da319a1b30a8474521d2d60c1b0bbf778fe3522e63f3a919efc03a8", strings.NewReader(string(message)))
	request.Header.Add("Content-Type", "application/json; charset=utf-8")
	response, err2 := client.Do(request)

	defer response.Body.Close()
	if err != nil {
		log.Errorf(err.Error())
	}
	if err2 != nil {
		log.Errorf(err2.Error())
	}

	if response.StatusCode == 200 {
		body, _ := ioutil.ReadAll(response.Body)
		log.Infof("%s\n", string(body))
	}
}

func GetJobLogList(query bson.M, page, size int, sort string) (list []*cronsun.JobLog, total int, err error) {
	err = mgoDB.WithC("job_log", func(c *mgo.Collection) error {
		total, err = c.Find(query).Count()
		if err != nil {
			return err
		}
		return c.Find(query).Select(selectForJobLogList).Sort(sort).Skip((page - 1) * size).Limit(size).All(&list)
	})
	return
}

func main() {
	flag.Parse()

	lcf := zap.NewDevelopmentConfig()
	lcf.Level.SetLevel(zapcore.Level(*level))
	lcf.Development = false
	logger, err := lcf.Build(zap.AddCallerSkip(1))
	log.SetLogger(logger.Sugar())
	if err != nil {
		slog.Fatalln("new log err:", err.Error())
	}
	// init config
	if err = conf.Init(*confFile, true); err != nil {
		log.Errorf("Init Config failed: %s", err)
		return
	}

	// init mongoDB
	if mgoDB, err = db.NewMdb(conf.Config.Mgo); err != nil {
		log.Errorf("Connect to MongoDB %s failed: %s",
			conf.Config.Mgo.Hosts, err)
		return
	}
	mgo.SetDebug(false)
	var aLogger *slog.Logger
	aLogger = slog.New(os.Stderr, "", slog.LstdFlags)
	mgo.SetLogger(aLogger)

	//wg := &sync.WaitGroup{}
	//wg.Add(1)
	cronInstance := cron.New()
	cronInstance.AddFunc("@every 10s", getLog)
	cronInstance.Start()
	defer cronInstance.Stop()

	// Cron should fire in 2 seconds. After 1 second, call Entries.
	select {
	case <-time.After(ONE_SECOND):
		cronInstance.Entries()
	}

	// Even though Entries was called, the cron should fire at the 2 second mark.
	select {
	case <-time.After(ONE_SECOND):

		//case <-wait():
		//fmt.Printf("wait")
	}
	// 注册退出事件
	event.On(event.EXIT)
	// 注册监听配置更新事件
	event.On(event.WAIT)
	// 监听退出信号
	event.Wait()
	// 处理退出事件
	event.Emit(event.EXIT, nil)
	log.Infof("exit success")
}
