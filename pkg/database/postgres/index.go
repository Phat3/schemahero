package postgres

import (
	"fmt"
	"strings"

	"github.com/lib/pq"
	schemasv1alpha3 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database/types"
)

func RemoveConstraintStatement(tableName string, index *types.Index) string {
	return fmt.Sprintf("alter table %s drop constraint %s", pq.QuoteIdentifier(tableName), pq.QuoteIdentifier(index.Name))
}

func RemoveIndexStatement(tableName string, index *types.Index) string {
	if index.IsUnique {
		return fmt.Sprintf("drop index if exists %s", pq.QuoteIdentifier(index.Name))
	}
	return fmt.Sprintf("drop index %s", pq.QuoteIdentifier(index.Name))
}

func AddIndexStatement(tableName string, schemaIndex *schemasv1alpha3.SQLTableIndex) string {
	unique := ""
	if schemaIndex.IsUnique {
		unique = "unique "
	}

	name := schemaIndex.Name
	if name == "" {
		name = types.GenerateIndexName(tableName, schemaIndex)
	}

	return fmt.Sprintf("create %sindex %s on %s (%s)",
		unique,
		name,
		tableName,
		strings.Join(schemaIndex.Columns, ", "))
}

func RenameIndexStatement(tableName string, index *types.Index, schemaIndex *schemasv1alpha3.SQLTableIndex) string {
	return fmt.Sprintf("alter index %s rename to %s", pq.QuoteIdentifier(index.Name), pq.QuoteIdentifier(schemaIndex.Name))
}
