package generate

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/interfaces"
	"github.com/schemahero/schemahero/pkg/database/mysql"
	"github.com/schemahero/schemahero/pkg/database/postgres"
	"github.com/schemahero/schemahero/pkg/database/types"
	"gopkg.in/yaml.v2"
)

type Generator struct {
	Driver    string
	URI       string
	DBName    string
	OutputDir string
}

func (g *Generator) RunSync() error {
	fmt.Printf("connecting to %s\n", g.URI)

	var db interfaces.SchemaHeroDatabaseConnection
	if g.Driver == "postgres" {
		pgDb, err := postgres.Connect(g.URI)
		if err != nil {
			return errors.Wrap(err, "failed to connect to postgres")
		}
		db = pgDb
	} else if g.Driver == "mysql" {
		mysqlDb, err := mysql.Connect(g.URI)
		if err != nil {
			return errors.Wrap(err, "failed to connect to mysql")
		}
		db = mysqlDb
	}

	tables, err := db.ListTables()
	if err != nil {
		return errors.Wrap(err, "failed to list tables")
	}

	filesWritten := make([]string, 0, 0)
	for _, table := range tables {
		primaryKey, err := db.GetTablePrimaryKey(table.Name)
		if err != nil {
			return errors.Wrap(err, "failed to get table primary key")
		}

		foreignKeys, err := db.ListTableForeignKeys(g.DBName, table.Name)
		if err != nil {
			return errors.Wrap(err, "failed to list table foreign keys")
		}

		indexes, err := db.ListTableIndexes(g.DBName, table.Name)
		if err != nil {
			return errors.Wrap(err, "failed to list table indexes")
		}

		columns, err := db.GetTableSchema(table.Name)
		if err != nil {
			return errors.Wrap(err, "failed to get table schema")
		}

		var primaryKeyColumns []string
		if primaryKey != nil {
			primaryKeyColumns = primaryKey.Columns
		}
		tableYAML, err := generateTableYAML(g.Driver, g.DBName, table, primaryKeyColumns, foreignKeys, indexes, columns)
		if err != nil {
			return errors.Wrap(err, "failed to generate table yaml")
		}

		// ensure that outputdir exists
		fi, err := os.Stat(g.OutputDir)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(g.OutputDir, 0755); err != nil {
				return errors.Wrap(err, "failed to create output dir")
			}
		} else if err != nil {
			return errors.Wrap(err, "failed to check if output dir exists")
		} else if !fi.IsDir() {
			return errors.New("output dir already exists and is not a directory")
		}

		// If there was a outputdir set, write it, else print it
		if g.OutputDir != "" {
			if err := ioutil.WriteFile(filepath.Join(g.OutputDir, fmt.Sprintf("%s.yaml", sanitizeName(table.Name))), []byte(tableYAML), 0644); err != nil {
				return err
			}

			filesWritten = append(filesWritten, fmt.Sprintf("./%s.yaml", sanitizeName(table.Name)))
		} else {

			fmt.Println(tableYAML)
			fmt.Println("---")
		}
	}

	// If there was an output-dir, write a kustomization.yaml too -- this should be optional
	if g.OutputDir != "" {
		kustomization := struct {
			Resources []string `yaml:"resources"`
		}{
			filesWritten,
		}

		kustomizeDoc, err := yaml.Marshal(kustomization)
		if err != nil {
			return err
		}

		if err := ioutil.WriteFile(filepath.Join(g.OutputDir, "kustomization.yaml"), kustomizeDoc, 0644); err != nil {
			return err
		}
	}
	return nil
}

func generateTableYAML(driver string, dbName string, table *types.Table, primaryKey []string, foreignKeys []*types.ForeignKey, indexes []*types.Index, columns []*types.Column) (string, error) {
	if driver == "mysql" {
		return generateMysqlTableYAML(dbName, table, primaryKey, foreignKeys, indexes, columns)
	}

	return generateGenericTableYAML(driver, dbName, table, primaryKey, foreignKeys, indexes, columns)
}

func generateMysqlTableYAML(dbName string, table *types.Table, primaryKey []string, foreignKeys []*types.ForeignKey, indexes []*types.Index, columns []*types.Column) (string, error) {
	schemaForeignKeys := make([]*schemasv1alpha4.SQLTableForeignKey, 0, 0)
	for _, foreignKey := range foreignKeys {
		schemaForeignKey := types.ForeignKeyToSchemaForeignKey(foreignKey)
		schemaForeignKeys = append(schemaForeignKeys, schemaForeignKey)
	}

	schemaIndexes := make([]*schemasv1alpha4.SQLTableIndex, 0, 0)
	for _, index := range indexes {
		schemaIndex := types.IndexToSchemaIndex(index)
		schemaIndexes = append(schemaIndexes, schemaIndex)
	}

	schemaTableColumns := make([]*schemasv1alpha4.MysqlSQLTableColumn, 0, 0)
	for _, column := range columns {
		schemaTableColumn, err := types.ColumnToMysqlSchemaColumn(column)
		if err != nil {
			fmt.Printf("%#v\n", err)
			return "", err
		}

		schemaTableColumns = append(schemaTableColumns, schemaTableColumn)
	}

	tableSchema := &schemasv1alpha4.MysqlSQLTableSchema{
		PrimaryKey:     primaryKey,
		Columns:        schemaTableColumns,
		ForeignKeys:    schemaForeignKeys,
		Indexes:        schemaIndexes,
		DefaultCharset: table.Charset,
		Collation:      table.Collation,
	}

	schema := &schemasv1alpha4.TableSchema{}

	schema.Mysql = tableSchema

	schemaHeroResource := schemasv1alpha4.TableSpec{
		Database: dbName,
		Name:     table.Name,
		Requires: []string{},
		Schema:   schema,
	}

	specDoc := struct {
		Spec schemasv1alpha4.TableSpec `yaml:"spec"`
	}{
		schemaHeroResource,
	}

	b, err := yaml.Marshal(&specDoc)
	if err != nil {
		fmt.Printf("%#v\n", err)
		return "", err
	}

	// TODO consider marshaling this instead of inline
	tableDoc := fmt.Sprintf(`apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: %s
%s`, sanitizeName(table.Name), b)

	return tableDoc, nil

}

func generateGenericTableYAML(driver string, dbName string, table *types.Table, primaryKey []string, foreignKeys []*types.ForeignKey, indexes []*types.Index, columns []*types.Column) (string, error) {
	schemaForeignKeys := make([]*schemasv1alpha4.SQLTableForeignKey, 0, 0)
	for _, foreignKey := range foreignKeys {
		schemaForeignKey := types.ForeignKeyToSchemaForeignKey(foreignKey)
		schemaForeignKeys = append(schemaForeignKeys, schemaForeignKey)
	}

	schemaIndexes := make([]*schemasv1alpha4.SQLTableIndex, 0, 0)
	for _, index := range indexes {
		schemaIndex := types.IndexToSchemaIndex(index)
		schemaIndexes = append(schemaIndexes, schemaIndex)
	}

	schemaTableColumns := make([]*schemasv1alpha4.SQLTableColumn, 0, 0)
	for _, column := range columns {
		schemaTableColumn, err := types.ColumnToSchemaColumn(column)
		if err != nil {
			fmt.Printf("%#v\n", err)
			return "", err
		}

		schemaTableColumns = append(schemaTableColumns, schemaTableColumn)
	}

	tableSchema := &schemasv1alpha4.SQLTableSchema{
		PrimaryKey:  primaryKey,
		Columns:     schemaTableColumns,
		ForeignKeys: schemaForeignKeys,
		Indexes:     schemaIndexes,
	}

	schema := &schemasv1alpha4.TableSchema{}

	if driver == "postgres" {
		schema.Postgres = tableSchema
	} else if driver == "cockroachdb" {
		schema.CockroachDB = tableSchema
	}

	schemaHeroResource := schemasv1alpha4.TableSpec{
		Database: dbName,
		Name:     table.Name,
		Requires: []string{},
		Schema:   schema,
	}

	specDoc := struct {
		Spec schemasv1alpha4.TableSpec `yaml:"spec"`
	}{
		schemaHeroResource,
	}

	b, err := yaml.Marshal(&specDoc)
	if err != nil {
		fmt.Printf("%#v\n", err)
		return "", err
	}

	// TODO consider marshaling this instead of inline
	tableDoc := fmt.Sprintf(`apiVersion: schemas.schemahero.io/v1alpha4
kind: Table
metadata:
  name: %s
%s`, sanitizeName(table.Name), b)

	return tableDoc, nil

}

func sanitizeName(name string) string {
	return strings.Replace(name, "_", "-", -1)
}
