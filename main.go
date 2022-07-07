package main

import (
	"flag"
	"log"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

var (
	startTime = time.Now().Unix()
	input     = flag.String("i", "", "m3u8 URL地址")
	output    = flag.String("o", "", "存储目录")
	threads   = flag.Int("t", 1, "协程数量")
	sleep     = flag.Duration("s", 0, "休眠时间")
	storage   string
)

func init() {
	flag.Parse()

	if ok, err := regexp.MatchString(`https?://[\s\S]+`, *input); err != nil || !ok {
		log.Fatal("m3u8 URL地址格式错误")
	}

	if *threads > 10000 || *threads < 1 {
		log.Fatal("协程数量错误")
	}

	var err error
	if storage, err = filepath.Abs(*output); err != nil {
		log.Fatal(err)
	}
}

func main() {
	log.Println("开始执行")

	// 处理索引内容
	content, err := handleM3u8(*input, storage, "")
	if err != nil {
		log.Fatal(err)
	}

	// 存储索引文件
	err = filePutContents(storage+"/index.m3u8", content)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("操作完成，用时 %ss\n", strconv.FormatInt(time.Now().Unix()-startTime, 10))
}
