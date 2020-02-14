// Copyright 2019 Copyright (c) 2019 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package result

import (
	"context"
	"fmt"
	"github.com/gardener/gardener/pkg/utils/kubernetes"
	"github.com/gardener/test-infra/pkg/common"
	"github.com/gardener/test-infra/pkg/util"
	"github.com/gardener/test-infra/pkg/util/elasticsearch"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/types"
	"os"
	"os/exec"
	"path/filepath"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tmv1beta1 "github.com/gardener/test-infra/pkg/apis/testmachinery/v1beta1"
)

const (
	cliPath = "/cc/utils/cli.py"
)

func IngestDir(log logr.Logger, path string, esCfgName string, esConfig *elasticsearch.Config) error {
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return fmt.Errorf("cannot read directory '%s'd: %s", path, err.Error())
	}
	for _, file := range files {
		if !file.IsDir() {
			err = IngestFile(log.WithValues("file", filepath.Join(path, file.Name())), filepath.Join(path, file.Name()), esCfgName, esConfig)
			if err != nil {
				log.Error(err, "error while trying to ingest file", "file", filepath.Join(path, file.Name()))
			}
		}
	}
	return nil
}

// IngestFile takes the summary file generated by the output and uploads it with the cc-utils cli command to elasticsearch
func IngestFile(log logr.Logger, file, esCfgName string, esConfig *elasticsearch.Config) error {
	log.Info("Ingest file into elasticsearch")

	if esConfig != nil {
		esClient, err := elasticsearch.NewClient(*esConfig)
		if err != nil {
			return err
		}
		if err := esClient.BulkFromFile(file); err != nil {
			return err
		}
		return nil
	}
	// keep both implementations for migration across landscapes
	if esCfgName != "" {
		return ingestFileOld(log, file, esCfgName)
	}
	return nil
}

func ingestFileOld(log logr.Logger, file, esCfgName string) error {
	cmd := exec.Command(cliPath, "elastic", "store_bulk", "--cfg-name", esCfgName, "--file", file)
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return err
	}
	log.Info("File successfully ingested to elasticsearch")
	return nil
}

// MarkTestrunsAsIngested sets the ingest status of testruns to true
func MarkTestrunsAsIngested(log logr.Logger, tmClient client.Client, tr *tmv1beta1.Testrun) error {
	ctx := context.Background()
	defer ctx.Done()

	patched := tr.DeepCopy()
	util.SetMetaDataLabel(&patched.ObjectMeta, common.LabelIngested, "true")
	patch, err := kubernetes.CreateTwoWayMergePatch(tr, patched)
	if err != nil {
		return errors.Wrap(err, "unable to create patch update for ingestion")
	}

	if err := tmClient.Patch(ctx, tr, client.ConstantPatch(types.MergePatchType, patch)); err != nil {
		return fmt.Errorf("unable to update status of testrun %s in namespace %s: %s", tr.Name, tr.Namespace, err.Error())
	}
	log.V(3).Info("Successfully updated status of testrun")

	return nil
}
