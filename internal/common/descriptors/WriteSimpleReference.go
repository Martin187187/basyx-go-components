package descriptors

import (
	"database/sql"
	"encoding/json"

	"github.com/eclipse-basyx/basyx-go-components/internal/common/model"
	persistenceutils "github.com/eclipse-basyx/basyx-go-components/internal/submodelrepository/persistence/utils"
)

// CreateSimpleReference
func CreateSimpleReference(tx *sql.Tx, semanticID *model.Reference) (sql.NullInt64, error) {
	var id int
	var referenceID sql.NullInt64

	insertKeyQuery := `INSERT INTO reference_simple_key (reference_id, position, type, value) VALUES ($1, $2, $3, $4)`

	if semanticID != nil && !persistenceutils.IsEmptyReference(*semanticID) {

		referredValue, err := PackReference(semanticID.ReferredSemanticID)
		if err != nil {
			return sql.NullInt64{}, err
		}
		err = tx.QueryRow(`INSERT INTO reference_simple (type, referedReference) VALUES ($1, $2) RETURNING id`, semanticID.Type, referredValue).Scan(&id)
		if err != nil {
			return sql.NullInt64{}, err
		}
		referenceID = sql.NullInt64{Int64: int64(id), Valid: true}

		references := semanticID.Keys
		for i := range references {
			_, err = tx.Exec(insertKeyQuery,
				id, i, references[i].Type, references[i].Value)
			if err != nil {
				return sql.NullInt64{}, err
			}
		}

		if err != nil {
			return sql.NullInt64{}, err
		}
	}
	return referenceID, nil
}

func PackReference(reference *model.Reference) (any, error) {
	if reference != nil {
		b, err := json.Marshal(reference)
		if err != nil {
			return sql.NullInt64{}, err
		}
		return b, nil
	} else {
		return nil, nil
	}
}

func PackReferences(refs []model.Reference) (any, error) {
	if refs != nil {
		b, err := json.Marshal(refs)
		if err != nil {
			return sql.NullInt64{}, err
		}
		return b, nil
	}
	return nil, nil
}
