# download

方便下载 m3u8 视频而开发的（并发下载），写得很丑，但勉强能用

## 编译

```
go install
```

## 使用

```
# -i m3u8 URL地址
# -o 存储目录
# -t 协程数量
# -s 休眠时间
download.exe -i http://example.com/video/10000/index.m3u8
```
