package sync

import (
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"

	"github.com/zeebo/blake3"
)

// =============================================================
// Verificação de Integridade Pós-Transferência
// =============================================================
//
// Após receber e gravar um arquivo, verificamos que o hash do
// arquivo no disco corresponde ao hash esperado (enviado pelo
// remetente). Isso garante que:
//   - Não houve corrupção durante a transferência de rede
//   - O arquivo foi gravado corretamente no disco
//   - Não houve interferência de outro processo durante a escrita

// IntegrityError representa um erro de integridade de dados
type IntegrityError struct {
	FilePath     string
	ExpectedHash string
	ActualHash   string
	FileSize     int64
}

func (e *IntegrityError) Error() string {
	return fmt.Sprintf("falha de integridade em %s: esperado=%s, obtido=%s (tamanho=%d)",
		e.FilePath, e.ExpectedHash, e.ActualHash, e.FileSize)
}

// IntegrityResult contém o resultado de uma verificação de integridade
type IntegrityResult struct {
	Valid        bool   `json:"valid"`
	FilePath     string `json:"file_path"`
	ExpectedHash string `json:"expected_hash"`
	ActualHash   string `json:"actual_hash"`
	FileSize     int64  `json:"file_size"`
	Error        string `json:"error,omitempty"`
}

// VerifyFileIntegrity verifica se o hash do arquivo no disco corresponde ao hash esperado.
// Usa BLAKE3 para calcular o hash, consistente com o restante do sistema.
func VerifyFileIntegrity(fullPath string, expectedHash string) *IntegrityResult {
	result := &IntegrityResult{
		FilePath:     fullPath,
		ExpectedHash: expectedHash,
	}

	// Se não temos hash esperado, não há como verificar
	if expectedHash == "" {
		result.Valid = true
		result.Error = "sem hash esperado para verificação"
		return result
	}

	f, err := os.Open(fullPath)
	if err != nil {
		result.Error = fmt.Sprintf("erro ao abrir arquivo: %v", err)
		return result
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		result.Error = fmt.Sprintf("erro ao obter info do arquivo: %v", err)
		return result
	}
	result.FileSize = info.Size()

	// Calcular hash BLAKE3 do arquivo no disco
	h := blake3.New()
	if _, err := io.Copy(h, f); err != nil {
		result.Error = fmt.Sprintf("erro ao calcular hash: %v", err)
		return result
	}

	result.ActualHash = hex.EncodeToString(h.Sum(nil))
	result.Valid = result.ActualHash == expectedHash

	if !result.Valid {
		log.Printf("[Integridade] FALHA: %s esperado=%s obtido=%s",
			fullPath, expectedHash, result.ActualHash)
	}

	return result
}

// VerifyChunkIntegrity verifica se os dados de um chunk correspondem ao hash esperado.
// Usado para verificar cada bloco CDC recebido antes de gravar no disco.
func VerifyChunkIntegrity(data []byte, expectedHash string) *IntegrityResult {
	result := &IntegrityResult{
		ExpectedHash: expectedHash,
		FileSize:     int64(len(data)),
	}

	if expectedHash == "" {
		result.Valid = true
		result.Error = "sem hash esperado para verificação"
		return result
	}

	h := blake3.Sum256(data)
	result.ActualHash = hex.EncodeToString(h[:])
	result.Valid = result.ActualHash == expectedHash

	if !result.Valid {
		log.Printf("[Integridade] FALHA em chunk: esperado=%s obtido=%s (tamanho=%d)",
			expectedHash, result.ActualHash, len(data))
	}

	return result
}

// VerifyDataIntegrity verifica se dados em memória correspondem ao hash esperado.
// Útil para verificar dados antes de gravar no disco.
func VerifyDataIntegrity(data []byte, expectedHash string) bool {
	if expectedHash == "" {
		return true // sem hash para comparar
	}

	h := blake3.Sum256(data)
	actualHash := hex.EncodeToString(h[:])
	return actualHash == expectedHash
}

// ComputeFileHash calcula o hash BLAKE3 de um arquivo.
// Wrapper simplificado para uso externo.
func ComputeFileHash(fullPath string) (string, error) {
	return hashFile(fullPath)
}

// ComputeDataHash calcula o hash BLAKE3 de dados em memória.
func ComputeDataHash(data []byte) string {
	h := blake3.Sum256(data)
	return hex.EncodeToString(h[:])
}

// IntegrityReport sumariza verificações de integridade de uma sessão de sync
type IntegrityReport struct {
	TotalChecks int               `json:"total_checks"`
	Passed      int               `json:"passed"`
	Failed      int               `json:"failed"`
	Skipped     int               `json:"skipped"` // sem hash disponível
	Failures    []IntegrityResult `json:"failures,omitempty"`
}

// NewIntegrityReport cria um novo relatório vazio
func NewIntegrityReport() *IntegrityReport {
	return &IntegrityReport{}
}

// AddResult adiciona um resultado ao relatório
func (ir *IntegrityReport) AddResult(result *IntegrityResult) {
	ir.TotalChecks++
	if result.ExpectedHash == "" {
		ir.Skipped++
	} else if result.Valid {
		ir.Passed++
	} else {
		ir.Failed++
		ir.Failures = append(ir.Failures, *result)
	}
}

// String retorna resumo do relatório
func (ir *IntegrityReport) String() string {
	return fmt.Sprintf("Integridade: %d verificações, %d OK, %d falhas, %d ignorados",
		ir.TotalChecks, ir.Passed, ir.Failed, ir.Skipped)
}
