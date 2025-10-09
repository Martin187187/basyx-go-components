/*******************************************************************************
* Copyright (C) 2025 the Eclipse BaSyx Authors and Fraunhofer IESE
*
* Permission is hereby granted, free of charge, to any person obtaining
* a copy of this software and associated documentation files (the
* "Software"), to deal in the Software without restriction, including
* without limitation the rights to use, copy, modify, merge, publish,
* distribute, sublicense, and/or sell copies of the Software, and to
* permit persons to whom the Software is furnished to do so, subject to
* the following conditions:
*
* The above copyright notice and this permission notice shall be
* included in all copies or substantial portions of the Software.
*
* THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
* EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF
* MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
* NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
* LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION
* OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION
* WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
*
* SPDX-License-Identifier: MIT
******************************************************************************/

/*
 * DotAAS Part 1 | Metamodel | Schemas
 *
 * The schemas implementing the [Specification of the Asset Administration Shell: Part 1](https://industrialdigitaltwin.org/en/content-hub/aasspecifications).   Copyright: Industrial Digital Twin Association (IDTA) 2025
 *
 * API version: V3.1.1
 * Contact: info@idtwin.org
 */

package model

type RelationshipElementChoice struct {
	Extensions []Extension `json:"extensions,omitempty"`

	Category string `json:"category,omitempty"`

	IdShort ReferableAllOfIdShort `json:"idShort,omitempty"`

	DisplayName []LangStringNameType `json:"displayName,omitempty"`

	Description []LangStringTextType `json:"description,omitempty"`

	ModelType *interface{} `json:"modelType"`

	SemanticId *Reference `json:"semanticId,omitempty"`

	SupplementalSemanticIds []Reference `json:"supplementalSemanticIds,omitempty"`

	Qualifiers []Qualifier `json:"qualifiers,omitempty"`

	EmbeddedDataSpecifications []EmbeddedDataSpecification `json:"embeddedDataSpecifications,omitempty"`

	First *Reference `json:"first,omitempty"`

	Second *Reference `json:"second,omitempty"`

	Annotations []DataElementChoice `json:"annotations,omitempty"`
}

// AssertRelationshipElementChoiceRequired checks if the required fields are not zero-ed
func AssertRelationshipElementChoiceRequired(obj RelationshipElementChoice) error {
	elements := map[string]interface{}{
		"modelType": obj.ModelType,
	}
	for name, el := range elements {
		if isZero := IsZeroValue(el); isZero {
			return &RequiredError{Field: name}
		}
	}

	for _, el := range obj.Extensions {
		if err := AssertExtensionRequired(el); err != nil {
			return err
		}
	}
	for _, el := range obj.DisplayName {
		if err := AssertLangStringNameTypeRequired(el); err != nil {
			return err
		}
	}
	for _, el := range obj.Description {
		if err := AssertLangStringTextTypeRequired(el); err != nil {
			return err
		}
	}
	if obj.SemanticId != nil {
		if err := AssertReferenceRequired(*obj.SemanticId); err != nil {
			return err
		}
	}
	for _, el := range obj.SupplementalSemanticIds {
		if err := AssertReferenceRequired(el); err != nil {
			return err
		}
	}
	for _, el := range obj.Qualifiers {
		if err := AssertQualifierRequired(el); err != nil {
			return err
		}
	}
	for _, el := range obj.EmbeddedDataSpecifications {
		if err := AssertEmbeddedDataSpecificationRequired(el); err != nil {
			return err
		}
	}
	if obj.First != nil {
		if err := AssertReferenceRequired(*obj.First); err != nil {
			return err
		}
	}
	if obj.Second != nil {
		if err := AssertReferenceRequired(*obj.Second); err != nil {
			return err
		}
	}
	for _, el := range obj.Annotations {
		if err := AssertDataElementChoiceRequired(el); err != nil {
			return err
		}
	}
	return nil
}

// AssertRelationshipElementChoiceConstraints checks if the values respects the defined constraints
func AssertRelationshipElementChoiceConstraints(obj RelationshipElementChoice) error {
	for _, el := range obj.Extensions {
		if err := AssertExtensionConstraints(el); err != nil {
			return err
		}
	}
	for _, el := range obj.DisplayName {
		if err := AssertLangStringNameTypeConstraints(el); err != nil {
			return err
		}
	}
	for _, el := range obj.Description {
		if err := AssertLangStringTextTypeConstraints(el); err != nil {
			return err
		}
	}
	if obj.SemanticId != nil {
		if err := AssertReferenceConstraints(*obj.SemanticId); err != nil {
			return err
		}
	}
	for _, el := range obj.SupplementalSemanticIds {
		if err := AssertReferenceConstraints(el); err != nil {
			return err
		}
	}
	for _, el := range obj.Qualifiers {
		if err := AssertQualifierConstraints(el); err != nil {
			return err
		}
	}
	for _, el := range obj.EmbeddedDataSpecifications {
		if err := AssertEmbeddedDataSpecificationConstraints(el); err != nil {
			return err
		}
	}
	if obj.First != nil {
		if err := AssertReferenceConstraints(*obj.First); err != nil {
			return err
		}
	}
	if obj.Second != nil {
		if err := AssertReferenceConstraints(*obj.Second); err != nil {
			return err
		}
	}
	for _, el := range obj.Annotations {
		if err := AssertDataElementChoiceConstraints(el); err != nil {
			return err
		}
	}
	return nil
}
