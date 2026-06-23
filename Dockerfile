# ── 构建阶段 ──
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags "-s -w" -o /babynas .

# ── 运行阶段（极简）──
FROM alpine:3.20
RUN apk add --no-cache tzdata && cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY --from=build /babynas /babynas
EXPOSE 8088
# 媒体目录与数据库通过挂载卷传入
# docker run -p 8088:8088 -v /nas/media:/media -v /nas/babynas:/data babynas \
#   -audio /media/音频 -video /media/视频 -db /data/babynas.db
ENTRYPOINT ["/babynas", "-addr", ":8088"]
