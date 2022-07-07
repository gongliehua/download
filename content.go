package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 链接格式
type link struct {
	originUrl   string
	downloadUrl string
	localPath   string
}

// 处理m3u8文件
// url m3u8文件地址，dir 文件存储目录，prefix 存储在索引文件中的ts前缀
func handleM3u8(url string, dir string, prefix string) (string, error) {
	log.Println("正在获取索引文件内容")
	// 获取索引内容
	content, err := getUrlContent(url)
	if err != nil {
		return "", err
	}

	// 多码率适配流（默认拿第一个）
	res := childIndex(url, content)
	if len(res) > 0 {
		log.Println("多码率适配流，正在处理：" + res[0])
		return handleM3u8(res[0], dir, prefix)
	}

	log.Println("正在匹配索引文件内容")
	// 替换索引内容
	// 获取需要抓取的地址
	var links []link
	// 匹配密钥文件的规则
	re := regexp.MustCompile(`URI="([\s\S]+)"`)
	// 按数字命名.ts文件
	tsIndex := 10000
	// 逐行处理，并替换内容
	for key, line := range strings.Split(content, "\n") {
		// 剔除前后空格方便判断
		line = strings.TrimSpace(line)
		// 如果第一行非指定内容，那么说明非m3u8文件
		if key == 0 && line != "#EXTM3U" {
			return "", errors.New("索引文件格式错误：" + line)
		}
		// 排除空行
		if line == "" {
			continue
		}
		// 判断前几行中是否存在密钥文件 || 普通链接
		if strings.HasPrefix(line, "#") {
			// 只去匹配前几行，后面就没必要匹配了
			if key <= 10 {
				matches := re.FindStringSubmatch(line)
				if len(matches) == 2 {
					href, err := url2absolute(url, matches[1])
					if err != nil {
						return "", err
					}
					tsIndex++
					tsIndexName := fmt.Sprintf(prefix+"%05d.ts", tsIndex)
					links = append(links, link{originUrl: matches[1], downloadUrl: href, localPath: tsIndexName})
					// 此处密钥文件可以说是完全匹配，没必要像下面那样操作
					content = strings.ReplaceAll(content, matches[1], tsIndexName)
				}
			}
		} else {
			href, err := url2absolute(url, line)
			if err != nil {
				return "", err
			}
			tsIndex++
			tsIndexName := fmt.Sprintf(prefix+"%05d.ts", tsIndex)
			links = append(links, link{originUrl: line, downloadUrl: href, localPath: tsIndexName})
			// 怕误伤，所以加个正则替换
			// 还好正则替换支持 $i 写法，不然我都不想写下去了
			// go 正则是不支持 (?<) 元字符的，之前写相对地址转绝对有用到，还好 go 有自带库处理（似乎功能不太完善），不然真写不下去
			reLine := regexp.MustCompile("(\\s+?)" + str2regexp(line) + "(\\s+?)")
			content = reLine.ReplaceAllString(content, "${1}"+tsIndexName+"${2}")
		}
	}

	// 抓取内容
	// 总任务数
	taskTotal := len(links)
	log.Printf("共计 %d 条下载任务\n", taskTotal)
	log.Println("开始抓取内容")
	// 存储ts文件的目录
	var tsDir string
	if prefix != "" {
		tsDir = dir + "/" + strings.TrimRight(prefix, "/\\")
	} else {
		tsDir = dir
	}
	// 预先处理，在下载时就不必重复操作，而提升性能
	err = fixMkdirAll(tsDir)
	if err != nil {
		log.Fatal(err)
	}
	// 计数通道
	countChan := make(chan int, *threads)
	// 执行计数
	go countStatMsg(*threads, countChan)
	// 开始下载
	var wg sync.WaitGroup
	wg.Add(*threads)
	for i := 1; i <= *threads; i++ {
		taskIndex := processAvgNum(taskTotal, *threads, i)
		go handleLinks(&wg, countChan, i, links[taskIndex[0]:taskIndex[1]], dir)
	}
	wg.Wait()

	return content, nil
}

// 判断多码率适配流
func childIndex(pageUrl, content string) []string {
	data := []string{}
	re := regexp.MustCompile(`#EXT-X-STREAM-INF.+?[\r\n]+(.+)`)
	matches := re.FindAllStringSubmatch(content, -1)
	if len(matches) > 0 {
		for _, v := range matches {
			res, err := url2absolute(pageUrl, v[1])
			if err != nil {
				continue
			}
			data = append(data, res)
		}
	}
	return data
}

// 获取 URL 内容
func getUrlContent(url string) (string, error) {
	client := http.Client{Timeout: 300 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// 相对地址转绝对
func url2absolute(pageUrl string, href string) (string, error) {
	u, err := url.Parse(pageUrl)
	if err != nil {
		return "", err
	}

	ref, err := url.Parse(href)
	if err != nil {
		return "", err
	}

	r := u.ResolveReference(ref)

	return r.String(), nil
}

// 字符串转可用于正则的字符串
func str2regexp(str string) string {
	search := []string{"\\", "^", "$", ".", "+", "*", "?", "[", "]", "(", ")", "{", "}"}
	replace := []string{"\\\\", "\\^", "\\$", "\\.", "\\+", "\\*", "\\?", "\\[", "\\]", "\\(", "\\)", "\\{", "\\}"}
	for key, value := range search {
		str = strings.ReplaceAll(str, value, replace[key])
	}
	return str
}

// 创建目录
func fixMkdirAll(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		err = os.MkdirAll(path, 0666)
		if err != nil {
			return err
		}
		return nil
	}
	return err
}

// 计数统计
// threads 共计协程数，countChan 计数通道
func countStatMsg(threads int, countChan chan int) {
	defer close(countChan)
	total := 0
	lenThreads := strconv.Itoa(len(strconv.Itoa(threads)))
	str := "第 %0" + lenThreads + "d 号协程完成，当前进度：%0" + lenThreads + "d/%d\n"
	for i := 1; i <= threads; i++ {
		num := <-countChan
		total++
		log.Printf(str, num, total, threads)
	}
}

// 平均分配任务数
func processAvgNum(totalTaskNum int, totalProcessNum int, currentProcessNum int) []int {
	// 任务数小于程序数
	if totalTaskNum < totalProcessNum {
		if currentProcessNum > totalTaskNum {
			return []int{0, 0}
		} else {
			return []int{currentProcessNum - 1, currentProcessNum}
		}
	}

	// 每个程序平均拿多少任务
	avgInt := totalTaskNum / totalProcessNum

	// 平分后是否还有多余任务
	avgOver := totalTaskNum % totalProcessNum

	// 计算起始位置(偏移量)
	offset := (currentProcessNum - 1) * avgInt

	// fix 长时间卡在最后一个程序
	// 不应该把多余的任务给最后一个程序，而是应该再次把多余的任务平均分配给程序
	if avgOver >= 1 {
		if currentProcessNum <= avgOver {
			intOver := (currentProcessNum - 1) * 1
			return []int{offset + intOver, offset + intOver + avgInt + 1}
		} else {
			return []int{offset + avgOver, offset + avgOver + avgInt}
		}
	} else {
		return []int{offset, offset + avgInt}
	}
}

// 处理链接(下载)
func handleLinks(wg *sync.WaitGroup, countChan chan int, i int, links []link, dir string) {
	defer wg.Done()
	defer func() {
		countChan <- i
	}()

	// 重试次数
	retryNum := 5
	// 循环下载
	for _, value := range links {
		// 发生错误重试几次
		for r := 1; r <= retryNum; r++ {
			err := downloadFile(value.downloadUrl, dir+"/"+value.localPath)
			if err != nil {
				if r == retryNum {
					log.Println("下载失败，源地址：" + value.originUrl + "，链接：" + value.downloadUrl + "，位置：" + dir + "/" + value.localPath)
					break
				} else {
					time.Sleep(2 * time.Second)
					continue
				}
			}
			break
		}
		// 休眠、控制下载速率
		time.Sleep(*sleep)
	}
}

// 下载文件
func downloadFile(url string, path string) error {
	client := http.Client{Timeout: 300 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	if err != nil {
		return err
	}

	return nil
}

// 存储文件
func filePutContents(path string, content string) error {
	dir, _ := filepath.Split(path)
	err := fixMkdirAll(dir)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0666)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(content)
	if err != nil {
		return err
	}

	return nil
}
