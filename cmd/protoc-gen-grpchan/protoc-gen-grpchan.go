// Command protoc-gen-grpchan is a protoc plugin that generates gRPC client stubs
// in Go that use github.com/fullstorydev/grpchan.Channel as their transport
// abstraction, instead of using *grpc.ClientConn. This can be used to carry RPC
// requests and streams over other transports, such as HTTP 1.1 or in-process.
package main

import (
	"fmt"
	"path"

	"github.com/jhump/gopoet"
	"github.com/jhump/goprotoc/plugins"
	"github.com/jhump/protoreflect/desc"
)

func main() {
	plugins.PluginMain(doCodeGen)
}

func doCodeGen(req *plugins.CodeGenRequest, resp *plugins.CodeGenResponse) error {
	var names plugins.GoNames
	for _, fd := range req.Files {
		if err := generateChanStubs(fd, &names, resp); err != nil {
			return fmt.Errorf("%s: %v", fd.GetName(), err)
		}
	}
	return nil
}

var typeOfRegistry = gopoet.NamedType(gopoet.NewSymbol("github.com/fullstorydev/grpchan", "ServiceRegistry"))
var typeOfChannel = gopoet.NamedType(gopoet.NewSymbol("github.com/fullstorydev/grpchan", "Channel"))
var typeOfContext = gopoet.NamedType(gopoet.NewSymbol("golang.org/x/net/context", "Context"))
var typeOfCallOptions = gopoet.SliceType(gopoet.NamedType(gopoet.NewSymbol("google.golang.org/grpc", "CallOption")))

func generateChanStubs(fd *desc.FileDescriptor, names *plugins.GoNames, resp *plugins.CodeGenResponse) error {
	if len(fd.GetServices()) == 0 {
		return nil
	}

	pkg := names.GoPackageForFile(fd)
	filename := names.OutputFilenameFor(fd, ".pb.grpchan.go")
	f := gopoet.NewGoFile(path.Base(filename), pkg.ImportPath, pkg.Name)

	f.FileComment = "Code generated by protoc-gen-grpchan. DO NOT EDIT.\n" +
		"source: " + fd.GetName()

	for _, sd := range fd.GetServices() {
		svcName := names.CamelCase(sd.GetName())
		lowerSvcName := gopoet.Unexport(svcName)

		f.AddElement(gopoet.NewFunc(fmt.Sprintf("RegisterHandler%s", svcName)).
			AddArg("reg", typeOfRegistry).
			AddArg("srv", names.GoTypeForServiceServer(sd)).
			Printlnf("reg.RegisterService(&%s, srv)", names.GoNameOfServiceDesc(sd)))

		cc := gopoet.NewStructTypeSpec(fmt.Sprintf("%sChannelClient", lowerSvcName),
			gopoet.NewField("ch", typeOfChannel))
		f.AddType(cc)

		f.AddElement(gopoet.NewFunc(fmt.Sprintf("New%sChannelClient", svcName)).
			AddArg("ch", typeOfChannel).
			AddResult("", names.GoTypeForServiceClient(sd)).
			Printlnf("return &%s{ch: ch}", cc))

		streamCount := 0
		for _, md := range sd.GetMethods() {
			mtdName := names.CamelCase(md.GetName())
			if md.IsClientStreaming() {
				// bidi or client streaming method
				f.AddElement(gopoet.NewMethod(gopoet.NewPointerReceiverForType("c", cc), mtdName).
					AddArg("ctx", typeOfContext).
					AddArg("opts", typeOfCallOptions).
					SetVariadic(true).
					AddResult("", names.GoTypeForStreamClient(md)).
					AddResult("", gopoet.ErrorType).
					Printlnf("stream, err := c.ch.NewStream(ctx, &%s.Streams[%d], \"/%s/%s\", opts...)", names.GoNameOfServiceDesc(sd), streamCount, sd.GetFullyQualifiedName(), md.GetName()).
					Printlnf("if err != nil {").
					Printlnf("    return nil, err").
					Printlnf("}").
					Printlnf("x := &%s{stream}", names.GoTypeForStreamClientImpl(md)).
					Printlnf("return x, nil"))
				streamCount++
			} else if md.IsServerStreaming() {
				// server streaming method
				f.AddElement(gopoet.NewMethod(gopoet.NewPointerReceiverForType("c", cc), mtdName).
					AddArg("ctx", typeOfContext).
					AddArg("in", names.GoTypeOfRequest(md)).
					AddArg("opts", typeOfCallOptions).
					SetVariadic(true).
					AddResult("", names.GoTypeForStreamClient(md)).
					AddResult("", gopoet.ErrorType).
					Printlnf("stream, err := c.ch.NewStream(ctx, &%s.Streams[%d], \"/%s/%s\", opts...)", names.GoNameOfServiceDesc(sd), streamCount, sd.GetFullyQualifiedName(), md.GetName()).
					Printlnf("if err != nil {").
					Printlnf("    return nil, err").
					Printlnf("}").
					Printlnf("x := &%s{stream}", names.GoTypeForStreamClientImpl(md)).
					Printlnf("if err := x.ClientStream.SendMsg(in); err != nil {").
					Printlnf("    return nil, err").
					Printlnf("}").
					Printlnf("if err := x.ClientStream.CloseSend(); err != nil {").
					Printlnf("    return nil, err").
					Printlnf("}").
					Printlnf("return x, nil"))
				streamCount++
			} else {
				// unary method
				f.AddElement(gopoet.NewMethod(gopoet.NewPointerReceiverForType("c", cc), mtdName).
					AddArg("ctx", typeOfContext).
					AddArg("in", names.GoTypeOfRequest(md)).
					AddArg("opts", typeOfCallOptions).
					SetVariadic(true).
					AddResult("", names.GoTypeOfResponse(md)).
					AddResult("", gopoet.ErrorType).
					Printlnf("out := new(%s)", names.GoTypeForMessage(md.GetOutputType())).
					Printlnf("err := c.ch.Invoke(ctx, \"/%s/%s\", in, out, opts...)", sd.GetFullyQualifiedName(), md.GetName()).
					Printlnf("if err != nil {").
					Printlnf("    return nil, err").
					Printlnf("}").
					Printlnf("return out, nil"))
			}
		}
	}

	out := resp.OutputFile(filename)
	return gopoet.WriteGoFile(out, f)
}
