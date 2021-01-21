package main

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	proto "example.com/grpcImg/proto"
)

const (
	Size8GB           = 8 * 1024 * 1024 * 1024
	MaxUploadFileSize = Size8GB
	Size4MB           = 4 * 1024 * 1024
	port              = ":50051"
	timestampFormat   = time.StampNano
)

type server struct {
	Dir string
}

func (s *server) List(ctx context.Context, in *proto.Empty) (*proto.FileInfoResponse, error) {
	filesInfo := make([]*proto.FileInfo, 0)
	files, err := ioutil.ReadDir(s.Dir)
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		fileinfo := proto.FileInfo{
			Name:   f.Name(),
			Size:   f.Size(),
			Create: int64(f.ModTime().Unix()),
		}
		filesInfo = append(filesInfo, &fileinfo)
	}

	return &proto.FileInfoResponse{Files: filesInfo}, nil
}

func (s *server) Upload(stream proto.Greeter_UploadServer) error {

	md, ok := metadata.FromIncomingContext(stream.Context())
	if !ok {
		return status.Errorf(codes.DataLoss, "ClientStreamingEcho: failed to get metadata")
	}

	filename := md["filename"][0]
	filesize, _ := strconv.Atoi(md["size"][0])
	log.Printf("Received Upload name: %v, size: %v", filename, filesize)

	size := 0
	file, err := os.OpenFile(path.Join(s.Dir, filename), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer file.Close()
	for {
		data, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			fmt.Println(err)
			return errors.Wrapf(err, "failed unexpectadely while reading chunks from stream")
		}
		size += len(data.Content)
		file.Write(data.Content)
	}
	statusCode := proto.StatusCode_Ok

	if size != filesize {
		statusCode = proto.StatusCode_Failed
	}
	err = stream.SendAndClose(&proto.Status{
		Code: statusCode,
	})
	return err
}

func (s *server) Download(req *proto.FileName, stream proto.Greeter_DownloadServer) error {
	log.Printf("Received Download")

	filepath := path.Join(s.Dir, req.Name)
	stats, err := os.Stat(filepath)

	if err != nil {
		log.Printf("could not greet: %v", err)
	}

	header := metadata.New(map[string]string{"filename": req.Name,
		"timestamp": time.Now().Format(timestampFormat),
		"size":      strconv.Itoa(int(stats.Size()))}) // send unix time
	stream.SendHeader(header)

	file, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer file.Close()
	buf := make([]byte, Size4MB)
	for {
		n, err := file.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return errors.Wrapf(err, "failed unexpectadely while reading chunks from stream")
		}

		err = stream.Send(&proto.Chunk{
			Content: buf[:n],
		})
		if err != nil {
			return errors.Wrapf(err, "failed unexpectadely while sending chunks to stream")
		}
	}
	return nil
}

func main() {
	dir := "./server/media"
	if len(os.Args) >= 2 {
		dir = os.Args[1]
	}
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer(grpc.MaxRecvMsgSize(MaxUploadFileSize))
	proto.RegisterGreeterServer(s, &server{
		Dir: dir,
	})
	fmt.Println("listen ", port)
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
