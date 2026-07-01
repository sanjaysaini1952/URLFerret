.PHONY: build build-linux build-windows clean

build:
	go build -o urlferret .

build-linux:
	GOOS=linux GOARCH=amd64 go build -o urlferret-linux-amd64 .

build-windows:
	GOOS=windows GOARCH=amd64 go build -o urlferret-windows-amd64.exe .

clean:
	rm -f urlferret urlferret.exe urlferret-linux-amd64 urlferret-windows-amd64.exe
