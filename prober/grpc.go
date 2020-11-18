// Copyright 2016 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prober

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/dynamic"
	"github.com/jhump/protoreflect/dynamic/grpcdynamic"
	"github.com/jhump/protoreflect/grpcreflect"
	"github.com/prometheus/blackbox_exporter/config"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	reflectpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
)

func ProbeGRPC(ctx context.Context, target string, module config.Module, registry *prometheus.Registry, logger log.Logger) bool {

	grpcProbeSuccessGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "grpc_probe_success",
		Help: "Displays whether or not the grpc probe was a success",
	})

	registry.MustRegister(grpcProbeSuccessGauge)

	creds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: module.GRPC.InsecureSkipVerify})
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
	}

	conn, err := grpc.DialContext(ctx, target, opts...)
	if err != nil {
		level.Error(logger).Log("msg", "Failed to craete GRCP connection", "err", err)
		return false
	}
	// initiate a new refClient
	refClient := grpcreflect.NewClient(ctx, reflectpb.NewServerReflectionClient(conn))

	// get file descriptors
	fileDesc, err := refClient.FileContainingSymbol(module.GRPC.Service)
	if err != nil {
		level.Info(logger).Log("msg", "Failed to get GRPC File Descriptor", "err", err)
		return false
	}

	// resolve methods
	svc, err := refClient.ResolveService(module.GRPC.Service)
	var methodDesc *desc.MethodDescriptor
	for _, m := range svc.GetMethods() {
		if m.GetName() == module.GRPC.Method {
			methodDesc = m
		}
	}
	if methodDesc == nil {
		level.Info(logger).Log("msg", fmt.Sprintf("Method %s is not found", module.GRPC.Method))
		return false
	}

	// construct dynamic message
	msg := dynamic.NewMessageFactoryWithDefaults()
	var messageDesc *desc.MessageDescriptor
	for _, m := range fileDesc.GetMessageTypes() {
		if m.GetName() == module.GRPC.MessageType {
			messageDesc = m
		}
	}
	if messageDesc == nil {
		level.Info(logger).Log("msg", fmt.Sprintf("Message Descriptor %s is not found", module.GRPC.MessageType))
		return false
	}
	message := msg.NewMessage(messageDesc)

	// initiate new stub
	stub := grpcdynamic.NewStub(conn)
	// invoke rpc and get result
	if s, err := stub.InvokeRpc(ctx, methodDesc, message); err != nil {
		panic(err)
	} else {
		if module.GRPC.ResponseMessage != "" && module.GRPC.ResponseMessage != s.String() {
			level.Info(logger).Log("msg", fmt.Sprintf("Response message %s != %s", s.String(), module.GRPC.ResponseMessage))
			return false
		}
	}

	grpcProbeSuccessGauge.Set(1)

	return true
}
