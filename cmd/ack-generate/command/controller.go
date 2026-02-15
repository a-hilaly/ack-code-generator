// Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package command

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	ackgenerate "github.com/aws-controllers-k8s/code-generator/pkg/generate/ack"
	ackmetadata "github.com/aws-controllers-k8s/code-generator/pkg/metadata"
	"github.com/aws-controllers-k8s/code-generator/pkg/sdk"
)

var (
	cmdControllerPath string
	pkgResourcePath   string
	latestAPIVersion  string
)

var controllerCmd = &cobra.Command{
	Use:   "controller <service>",
	Short: "Generates Go files containing service controller implementation for a given service",
	RunE:  generateController,
}

func init() {
	rootCmd.AddCommand(controllerCmd)
}

// generateController generates the Go files for a service controller
func generateController(cmd *cobra.Command, args []string) error {
	cmdStart := time.Now()
	if len(args) != 1 {
		return fmt.Errorf("please specify the service alias for the AWS service API to generate")
	}
	svcAlias := strings.ToLower(args[0])
	if optOutputPath == "" {
		optOutputPath = filepath.Join(optServicesDir, svcAlias)
	}

	fmt.Fprintf(os.Stderr, "\n[controller] EnsureRepo...\n")
	repoStart := time.Now()
	ctx, cancel := sdk.ContextWithSigterm(context.Background())
	defer cancel()
	sdkDirPath, err := sdk.EnsureRepo(ctx, optCacheDir, optRefreshCache, optAWSSDKGoVersion, optOutputPath)
	if err != nil {
		return err
	}
	sdkDir = sdkDirPath
	fmt.Fprintf(os.Stderr, "[controller] EnsureRepo: %s\n", time.Since(repoStart))

	fmt.Fprintf(os.Stderr, "\n[controller] loadModel...\n")
	modelStart := time.Now()
	metadata, err := ackmetadata.NewServiceMetadata(optMetadataConfigPath)
	if err != nil {
		return err
	}
	m, err := loadModelWithLatestAPIVersion(svcAlias, metadata)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[controller] loadModel: %s\n", time.Since(modelStart))

	serviceAccountName, err := getServiceAccountName()
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\n[controller] Controller() template setup...\n")
	ctrlStart := time.Now()
	ts, err := ackgenerate.Controller(m, optTemplateDirs, serviceAccountName)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[controller] Controller() template setup: %s\n", time.Since(ctrlStart))

	fmt.Fprintf(os.Stderr, "\n[controller] template execution...\n")
	execStart := time.Now()
	if err = ts.Execute(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "[controller] template execution: %s\n", time.Since(execStart))

	writeStart := time.Now()
	for path, contents := range ts.Executed() {
		if optDryRun {
			fmt.Printf("============================= %s ======================================\n", path)
			fmt.Println(strings.TrimSpace(contents.String()))
			continue
		}
		outPath := filepath.Join(optOutputPath, path)
		outDir := filepath.Dir(outPath)
		if _, err := sdk.EnsureDir(outDir); err != nil {
			return err
		}
		if err = ioutil.WriteFile(outPath, contents.Bytes(), 0666); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "[controller] file writing (%d files): %s\n", len(ts.Executed()), time.Since(writeStart))
	fmt.Fprintf(os.Stderr, "[controller] TOTAL: %s\n\n", time.Since(cmdStart))
	return nil
}
