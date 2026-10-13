// 文件用途：声明独立实验模块，避免依赖 Chat/GoFrame 主服务。
module memory-spike

go 1.26.0

require github.com/microsoft/go-mssqldb v1.11.2

require (
	github.com/golang-sql/civil v0.0.0-20220223132316-b832511892a9 // indirect
	github.com/golang-sql/sqlexp v0.1.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/text v0.41.0 // indirect
)
