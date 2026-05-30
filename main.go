package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	openrouter "github.com/OpenRouterTeam/go-sdk"
	"github.com/OpenRouterTeam/go-sdk/models/components"
	"github.com/OpenRouterTeam/go-sdk/optionalnullable"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/ryugenxd/docx2pdf"
)

var ai_client openrouter.OpenRouter

var PAPRA_AUTH_HEADER string = fmt.Sprintf("Bearer %v", os.Getenv("PAPRA_API_KEY"))
var PAPRA_DOMAIN string = os.Getenv("PAPRA_DOMAIN")

const SYSTEM_MESSAGE = `You are a document archiving assistant.

Input: a raw document file and available tags formatted as <tag name>:<tag id>.

Return only valid JSON:
{
  "content": "string",
  "name": "string",
  "tags": ["string"],
  "date": "Date", // nullable
  "exp_date": "Date" // nullable
}

Rules:
- Extract the document text accurately for indexing/search.
- Extract the document date, if present
- Extract the document expiry date, if applicable
- Never use the string value "null" for null values. Omit the field if it is null instead
- The Dates must be parseable using Node.js Date
- Keep extracted text in its original language. Do not translate, summarize, or invent content.
- Format the content in markdown
- Preserve key details: names, dates, amounts, IDs, addresses, organizations, invoice/certificate numbers.
- Use [unreadable] for unreadable text.
- Create a short, natural English name, without file extension.
- The name must not contain: / \ : * ? " < > |
- "tags" must contain only IDs from the provided tag list. Do not invent IDs.
- Use existing tags when appropriate.
- Output JSON only. No Markdown, comments, explanations, or trailing commas.`

func getUserMessage(tags []string) string {
	return fmt.Sprintf("Tags: %v", tags)
}

func main() {
	fmt.Println("Starting up...")

	OPENROUTER_API_KEY := os.Getenv("OPENROUTER_API_KEY")

	// Checking the env vars
	if OPENROUTER_API_KEY == "" {
		fmt.Println("OPENROUTER_API_KEY not set")
		os.Exit(1)
		return
	}

	if PAPRA_AUTH_HEADER == "Bearer " {
		fmt.Println("PAPRA_API_KEY is not set")
		os.Exit(1)
		return
	}
	if PAPRA_DOMAIN == "" {
		fmt.Println("PAPRA_DOMAIN is not set")
		os.Exit(1)
		return
	}

	ai_client = *openrouter.New(
		openrouter.WithSecurity(OPENROUTER_API_KEY),
	)

	app := fiber.New()
	app.Use(logger.New())
	app.Post("/process-document", processDocument)

	res := app.Listen(":3000")
	if res != nil {
		fmt.Printf("Exited with error: %v\n", res)
		os.Exit(1)
	}
}

type papraEvent struct {
	Data struct {
		DocumentId string  `json:"documentId"`
		OrgId      string  `json:"organizationId"`
		TagName    *string `json:"tagName"`
	} `json:"data"`
	Event string `json:"type"`
}

// TODO: Filter based on the triggering action
// TODO: Come up with the filters based on the actions
func processDocument(c fiber.Ctx) error {
	fmt.Printf("Processing document...")
	papraEvent := papraEvent{}
	err := json.Unmarshal(c.Body(), &papraEvent)
	if err != nil {
		fmt.Printf("Failed to parse para event request: %v", err)
		return c.Status(500).SendString("Failed to process body")
	}
	if (papraEvent.Event != "document:tag:added" || *papraEvent.Data.TagName != "To-Process") && papraEvent.Event != "document:created" {
		fmt.Printf("Unsupported trigger, skippng document...:\nEvent: %v\nTag: %v\n", papraEvent.Event, papraEvent.Data.TagName)
		return c.Status(200).SendString("Nothing to do")
	}

	doc, err := getDocument(papraEvent.Data.DocumentId, papraEvent.Data.OrgId)
	if err != nil {
		fmt.Printf("Failed to get document: %v\n", err)
		return c.Status(500).SendString("Failed to get document")
	}
	fmt.Println("Retrieved document")

	tags, err := getTags(doc.orgId)
	if err != nil {
		fmt.Println("Failed to retrieve tags")
		return c.Status(500).SendString("Failed to get tags")
	}
	fmt.Println("Retrieved tags")

	resp, err := ai_client.Chat.Send(c.Context(), components.ChatRequest{
		Messages: []components.ChatMessages{
			components.CreateChatMessagesSystem(
				components.ChatSystemMessage{
					Content: components.CreateChatSystemMessageContentStr(SYSTEM_MESSAGE),
					Role:    components.ChatSystemMessageRoleSystem,
				}),
			components.CreateChatMessagesUser(
				components.ChatUserMessage{
					Content: components.CreateChatUserMessageContentArrayOfChatContentItems([]components.ChatContentItems{
						components.CreateChatContentItemsText(components.ChatContentText{
							Text: getUserMessage(tags),
							Type: components.ChatContentTextTypeText,
						}),
						components.CreateChatContentItemsFile(components.ChatContentFile{
							File: components.File{
								FileData: &doc.content,
								Filename: &doc.name,
							},
						}),
					}),
				}),
		},
		Model: new("google/gemini-3.1-flash-lite"),
		Reasoning: &components.Reasoning{
			Effort: optionalnullable.OptionalNullable[components.Effort]{
				true: new(components.EffortMedium),
			},
			Summary: optionalnullable.OptionalNullable[components.ChatReasoningSummaryVerbosityEnum]{
				true: new(components.ChatReasoningSummaryVerbosityEnumConcise)},
		},
		ResponseFormat: new(components.CreateResponseFormatJSONSchema(components.ChatFormatJSONSchemaConfig{
			JSONSchema: components.ChatJSONSchemaConfig{
				Description: new("Document mapping"),
				Name:        "document_parse",
				Schema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{
							"type": "string",
						},
						"name": map[string]any{
							"type": "string",
						},
						"tags": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "string",
							},
						},
						// "suggestedTags": map[string]any{
						// 	"type": "array",
						// 	"items": map[string]any{
						// 		"type": "string",
						// 	},
						// },
						"date": map[string]any{
							"type": "string",
						},
						"exp_date": map[string]any{
							"type": "string",
						},
					},
					"required":             []string{"content", "name", "tags"},
					"additionalProperties": false,
				},
				Strict: optionalnullable.OptionalNullable[bool]{true: new(true)},
			},
			Type: "object",
		})),
	},
	)
	if err != nil {
		fmt.Printf("Chat completion failed: %v\n", err)
		return err
	}
	result, t := resp.ChatResult.Choices[0].GetMessage().Content.Get()
	if !t {
		fmt.Printf("No response was found")
		return c.Status(500).SendString("No LLM response found")
	}
	fmt.Println("Recieved AI response")
	s := result.Str

	llm_resp := Resp{}
	err = json.Unmarshal([]byte(*s), &llm_resp)
	if err != nil {
		fmt.Printf("Unable to parse LLM response: %v\n%v\n", *s, err)
		return err
	}

	fmt.Println("Updating document...")
	err = updateDocument(doc.documentId, doc.orgId, llm_resp)
	if err != nil {
		fmt.Printf("Failed to update the document: %v", err)
		return c.Status(500).SendString("Failed to update the document")
	}

	return c.Status(200).SendString("Accepted")
}

func convertPdf(docxContent []byte) ([]byte, error) {
	tmpDocx, err := os.CreateTemp("", "input_doc")
	if err != nil {
		return nil, fmt.Errorf("Failed to create docx temp file", err)
	}
	defer os.Remove(tmpDocx.Name())
	_, err = tmpDocx.Write(docxContent)
	if err != nil {
		tmpDocx.Close()
		return nil, fmt.Errorf("Failed to write docx document: %w", err)
	}
	tmpDocx.Close()
	tmpPdf, err := os.CreateTemp("", "out.pdf")
	if err != nil {
		return nil, fmt.Errorf("Failed to create pdf temp file: %w", err)
	}
	defer os.Remove(tmpPdf.Name())
	tmpPdf.Close()
	err = docx2pdf.ConvertFile(tmpDocx.Name(), tmpPdf.Name())
	tmpPdf, err = os.Open(tmpPdf.Name())
	if err != nil {
		return nil, fmt.Errorf("Failed to reopen PDF file: %w", err)
	}
	defer tmpPdf.Close()
	content, err := io.ReadAll(tmpPdf)
	if err != nil {
		return nil, fmt.Errorf("Failed to read PDF: %w", err)
	}
	return content, nil
}

func updateDocument(docId string, orgId string, new_params Resp) error {
	updateDocUrl := fmt.Sprintf("%v/api/organizations/%v/documents/%v", PAPRA_DOMAIN, orgId, docId)
	addTagUrl := fmt.Sprintf("%v/api/organizations/%v/documents/%v/tags", PAPRA_DOMAIN, orgId, docId)

	var updateRequestBody string
	if new_params.Date == nil || *new_params.Date == "" {
		updateRequestBody = fmt.Sprintf(`{"name":"%v","content":"%v"}`, new_params.Name, strings.ReplaceAll(new_params.Content, "\n", "\\n"))
	} else {
		updateRequestBody = fmt.Sprintf(`{"name":"%v","content":"%v","documentDate":"%v"}`, new_params.Name, strings.ReplaceAll(new_params.Content, "\n", "\\n"), *new_params.Date)
	}

	fmt.Printf("Update request body: %v\n", updateRequestBody)
	updateReq, err := http.NewRequest(http.MethodPatch, updateDocUrl, strings.NewReader(updateRequestBody))
	updateReq.Header.Set("Authorization", PAPRA_AUTH_HEADER)
	updateReq.Header.Set("Content-type", "application/json")
	if err != nil {
		return err
	}
	client := http.Client{}
	resp, err := client.Do(updateReq)
	if err != nil || resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		fmt.Printf("Failed to update document:\nerror: %v\nResponse: %v\n", err, string(body))
		return fmt.Errorf("Failed to update document: %w", err)
	}

	for _, tagId := range new_params.Tags {
		addTagBody := fmt.Sprintf(`{"tagId": "%v"}`, tagId)
		updateTagsReq, err := http.NewRequest(http.MethodPost, addTagUrl, strings.NewReader(addTagBody))
		updateTagsReq.Header.Set("Authorization", PAPRA_AUTH_HEADER)
		updateTagsReq.Header.Set("Content-type", "application/json")
		if err != nil {
			return fmt.Errorf("Failed to create tag request: %w", err)
		}
		createTagResp, err := client.Do(updateTagsReq)
		if err != nil || (createTagResp.StatusCode != 204 && createTagResp.StatusCode != 409) {
			b, _ := io.ReadAll(createTagResp.Body)
			fmt.Printf("Failed to update tag:\nError: %v\nResponse: %v\n", err, string(b))
			return fmt.Errorf("Failed to add tag: %w\n", err)
		}
	}

	// Update the document expiry date
	if new_params.ExpiryDate != nil && *new_params.ExpiryDate != "null" && *new_params.ExpiryDate != "" {
		setExpiryDateUrl := fmt.Sprintf("%v/api/organizations/%v/documents/%v/custom-properties/cpd_re5ufzny69rfe7pjn1v8mb2u", PAPRA_DOMAIN, orgId, docId)
		setExpiryDateBody := fmt.Sprintf(`{"value":"%v"}`, *new_params.ExpiryDate)
		setExpiryDateReq, err := http.NewRequest(http.MethodPut, setExpiryDateUrl, strings.NewReader(setExpiryDateBody))
		setExpiryDateReq.Header.Set("Authorization", PAPRA_AUTH_HEADER)
		setExpiryDateReq.Header.Set("Content-type", "application/json")
		setExpiryDateResp, err := client.Do(setExpiryDateReq)
		if err != nil || setExpiryDateResp.StatusCode != 204 {
			resp_body, err := io.ReadAll(setExpiryDateResp.Body)
			fmt.Printf("Failed to se expiry date:\nError: %v\nResponse: %v\nExpiry Date: %v\n", err, string(resp_body), *new_params.ExpiryDate)
			return fmt.Errorf("Failed to set the expiry date: %w", err)
		}
	}

	// Remove "To-Process" tag from the document
	removeTagUrl := fmt.Sprintf("%v/api/organizations/%v/documents/%v/tags/tag_p1dxu79tffeapj6uslmij4ts", PAPRA_DOMAIN, orgId, docId)
	removeTagReq, err := http.NewRequest(http.MethodDelete, removeTagUrl, nil)
	removeTagReq.Header.Set("Authorization", PAPRA_AUTH_HEADER)
	removeTagResp, err := client.Do(removeTagReq)
	if err != nil || removeTagResp.StatusCode != 204 {
		return fmt.Errorf("Failed to remove the process tag: %w", err)
	}
	return nil
}

type Resp struct {
	Content    string   `json:"content"`
	Name       string   `json:"name"`
	Tags       []string `json:"tags"`
	Date       *string  `json:"date"`
	ExpiryDate *string  `json:"exp_date"`
}

type TagsResp struct {
	Tags []struct {
		Id   string `json:"id"`
		Name string `json:"name"`
	} `json:"tags"`
}

func getTags(orgId string) ([]string, error) {
	tagsUrl := fmt.Sprintf("%v/api/organizations/%v/tags", PAPRA_DOMAIN, orgId)
	req, _ := http.NewRequest(http.MethodGet, tagsUrl, nil)
	req.Header.Set("Authorization", PAPRA_AUTH_HEADER)
	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		fmt.Printf("Failed to get tags: %v\n", err)
		return []string{}, err
	}
	res_body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("Failed to read tags body: %v\n", err)
		return nil, err
	}
	tags := TagsResp{}
	err = json.Unmarshal(res_body, &tags)
	if err != nil {
		fmt.Printf("Failed to parse the tags response: %v\n", err)
		return nil, err
	}
	tag_resp := make([]string, len(tags.Tags))
	for i, c := range tags.Tags {
		tag_resp[i] = fmt.Sprintf("%v:%v", c.Id, c.Name)
	}
	return tag_resp, nil
}

type PapraDocumentResponse struct {
	Document struct {
		OriginalName string `json:"originalName"`
		MimeType     string `json:"mimeType"`
		Name         string `json:"name"`
	} `json:"document"`
}

type document struct {
	// Base64 enconding of the file
	content string
	// The name in Papra
	name string
	// The document ID
	documentId string
	// The organization ID
	orgId string
}

func getDocument(docId string, orgId string) (*document, error) {
	docUrl := fmt.Sprintf("%v/api/organizations/%v/documents/%v", PAPRA_DOMAIN, orgId, docId)
	req, _ := http.NewRequest(http.MethodGet, docUrl, nil)
	req.Header.Set("Authorization", PAPRA_AUTH_HEADER)
	client := http.Client{}
	res, err := client.Do(req)
	if err != nil {
		log.Default().Printf("Failed to request document from papra: %v\n", err)
		return nil, err
	}

	docResp := PapraDocumentResponse{}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("Could not read the document response: %v\n", err)
		return nil, err
	}
	err = json.Unmarshal(body, &docResp)
	if err != nil {
		fmt.Printf("Could not unmarshal the response body: %v\n", err)
		return nil, err
	}

	fileUrl := fmt.Sprintf("%v/api/organizations/%v/documents/%v/file", PAPRA_DOMAIN, orgId, docId)
	req, _ = http.NewRequest(http.MethodGet, fileUrl, nil)
	req.Header.Set("Authorization", PAPRA_AUTH_HEADER)
	res, err = client.Do(req)
	if err != nil {
		fmt.Printf("Failed to get the document file: %v\n", err)
	}
	fileBody, err := io.ReadAll(res.Body)
	if err != nil {
		fmt.Printf("Failed to write to file: %v\n", err)
		return nil, err
	}

	// DOCX, convert it to PDf
	if docResp.Document.MimeType == "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		fileBody, err = convertPdf(fileBody)
		if err != nil {
			return nil, fmt.Errorf("Failed to convert DOCX to PDF: %w", err)
		}
		docResp.Document.MimeType = "application/pdf"
	}

	return &document{
		name:       docResp.Document.Name,
		content:    fmt.Sprintf("data:%v;base64,%v", docResp.Document.MimeType, base64.StdEncoding.EncodeToString(fileBody)),
		documentId: docId,
		orgId:      orgId,
	}, nil
}
