// +build linux

/*
http://www.apache.org/licenses/LICENSE-2.0.txt
Copyright 2016 Intel Corporation
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dbi

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/intelsdi-x/snap-plugin-collector-dbi/dbi/dtype"
	"github.com/intelsdi-x/snap-plugin-collector-dbi/dbi/parser"
	"github.com/intelsdi-x/snap-plugin-lib-go/v1/plugin"
)

// Hard coded because plugin-specific config isn't available in GetMetricTypes
const (
	setfilePath = "/opt/snap_plugins/etc/dbi-collector-plugin-config.json"
)

type QueryData struct {	
	value interface{}
	tags  map[string]string
}

// DbiPlugin holds information about the configuration database and defined queries
type DbiPlugin struct {
	databases   map[string]*dtype.Database
	queries     map[string]*dtype.Query
	initialized bool
}

// CollectMetrics returns values of desired metrics defined in mts
func (dbiPlg *DbiPlugin) CollectMetrics(mts []plugin.Metric) ([]plugin.Metric, error) {

	var err error
	metrics := []plugin.Metric{}
	data := map[string]QueryData{}

	// initialization - done once
	if dbiPlg.initialized == false {
		err = dbiPlg.setConfig()
		if err != nil {
			// Cannot obtained sql settings
			return nil, err
		}
		err = openDBs(dbiPlg.databases)
		if err != nil {
			return nil, err
		}
		dbiPlg.initialized = true
	} // end of initialization
	// execute dbs queries and get output
	data, err = dbiPlg.executeQueries()
	if err != nil {
		return nil, err
	}
	
	for i, m := range mts {
		if qdata, ok := data[m.Namespace.String()]; ok {
			mts[i].Timestamp = time.Now()
			mts[i].Data = qdata.value
			mts[i].Tags = qdata.tags
			metrics = append(metrics, mts[i])
		}

	}

	return metrics, nil
}

// GetConfigPolicy returns config policy
func (dbiPlg *DbiPlugin) GetConfigPolicy() (plugin.ConfigPolicy, error) {
	policy := plugin.NewConfigPolicy()
	return *policy, nil
}

// GetMetricTypes returns metrics types exposed by snap-plugin-collector-dbi
func (dbiPlg *DbiPlugin) GetMetricTypes(cfg plugin.Config) ([]plugin.Metric, error) {
	metrics := map[string]QueryData{}
	mts := []plugin.Metric{}

	err := dbiPlg.setConfig()
	if err != nil {
		// cannot obtained sql settings from Global Config
		return nil, err
	}

	metrics, err = dbiPlg.getMetrics()
	if err != nil {
		return nil, err
	}

	for name := range metrics {
		mts = append(mts, plugin.Metric{Namespace: plugin.NewNamespace(splitNamespace(name)...)})
	}

	return mts, nil
}

// New returns snap-plugin-collector-dbi instance
func New() *DbiPlugin {
	dbiPlg := &DbiPlugin{databases: map[string]*dtype.Database{}, queries: map[string]*dtype.Query{}, initialized: false}

	return dbiPlg
}

// setConfig extracts config item from Global Config or Metric Config, parses its contents (mainly information
// about databases and queries) and assigned them to appriopriate DBiPlugin fields
func (dbiPlg *DbiPlugin) setConfig() error {
	var err error
	dbiPlg.databases, dbiPlg.queries, err = parser.GetDBItemsFromConfig(setfilePath)
	if err != nil {
		// cannot parse sql config contents
		return err
	}

	return nil
}

// getMetrics returns map with dbi metrics values, where keys are metrics names
func (dbiPlg *DbiPlugin) getMetrics() (map[string]QueryData, error) {
	metrics := map[string]QueryData{}

	err := openDBs(dbiPlg.databases)

	if err != nil {
		return nil, err
	}

	// execute dbs queries and get statement outputs
	metrics, err = dbiPlg.executeQueries()
	if err != nil {
		return nil, err
	}

	errors := closeDBs(dbiPlg.databases)
	if errors != nil {
		var dbs []string
		for r := range errors {
			dbs = append(dbs, errors[r].Error())
		}
		return metrics, fmt.Errorf("Cannot close database(s):\n %s", dbs)
	}
	return metrics, nil
}

// executeQueries executes all defined queries of each database and returns results as map to its values,
// where keys are equal to columns' names
func (dbiPlg *DbiPlugin) executeQueries() (map[string]QueryData, error) {
	data := map[string]QueryData{}

	//execute queries for each defined databases
	for dbName, db := range dbiPlg.databases {
		if !db.Active {
			//skip if db is not active (none established connection)
			fmt.Fprintf(os.Stderr, "Cannot execute queries for database %s, is inactive (connection was not established properly)\n", dbName)
			continue
		}

		// retrive name from queries to be executed for this db
		for _, queryName := range db.QrsToExec {
			statement := dbiPlg.queries[queryName].Statement

			out, err := db.Executor.Query(queryName, statement)
			if err != nil {
				// log failing query and take the next one
				fmt.Fprintf(os.Stderr, "Cannot execute query %s for database %s", queryName, dbName)
				continue
			}

			for resName, res := range dbiPlg.queries[queryName].Results {
				instanceOk := false
				tagOk := false
				// to avoid inconsistency of columns names caused by capital letters (especially for postgresql driver)
				instanceFrom := strings.ToLower(res.InstanceFrom)
				valueFrom := strings.ToLower(res.ValueFrom)
				tagFrom := strings.ToLower(res.TagFrom)
        
				if !isEmpty(instanceFrom) {
					if len(out[instanceFrom]) == len(out[valueFrom]) {
						instanceOk = true
					}
				}

        if !isEmpty(tagFrom) {
          if len(out[tagFrom]) == len(out[valueFrom]) {
            tagOk = true
          }
        }

				for index, value := range out[valueFrom] {
					instance := ""
          tags := map[string]string{}

					if instanceOk {
						instance = fmt.Sprintf("%v", fixDataType(out[instanceFrom][index]))
					}

          if tagOk {
            tags[tagFrom] = fmt.Sprintf("%v", fixDataType(out[tagFrom][index]))
          }

					key := createNamespace(dbName, resName, res.InstancePrefix, instance)

					if _, exist := data[key]; exist {
						return nil, fmt.Errorf("Namespace `%s` has to be unique, but is not", key)
					}

					data[key] = QueryData{value: fixDataType(value), tags: tags}
				}
			}
		} // end of range db_queries_to_execute
	} // end of range databases

	if len(data) == 0 {
		return nil, fmt.Errorf("No data obtained from defined queries")
	}

	return data, nil
}

// fixDataType converts `arg` to a string if its type is an array of bytes or time.Time, in other case there is no change
func fixDataType(arg interface{}) interface{} {
	var result interface{}

	switch arg.(type) {
	case []byte:
		result = string(arg.([]byte))

	case time.Time:
		// gob: type time.Time is not registered for gob interface, conversion to string
		result = arg.(time.Time).String()

	default:
		result = arg
	}

	return result
}
