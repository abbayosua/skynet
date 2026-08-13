package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAttributionMigration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		configJSON       string
		expectedTrailer  TrailerStyle
		expectedGenerate bool
	}{
		{
			name: "old setting co_authored_by=true migrates to co-authored-by",
			configJSON: `{
				"options": {
					"attribution": {
						"co_authored_by": true,
						"generated_with": false
					}
				}
			}`,
			expectedTrailer:  TrailerStyleCoAuthoredBy,
			expectedGenerate: false,
		},
		{
			name: "old setting co_authored_by=false migrates to none",
			configJSON: `{
				"options": {
					"attribution": {
						"co_authored_by": false,
						"generated_with": true
					}
				}
			}`,
			expectedTrailer:  TrailerStyleNone,
			expectedGenerate: true,
		},
		{
			name: "new setting takes precedence over old setting",
			configJSON: `{
				"options": {
					"attribution": {
						"trailer_style": "assisted-by",
						"co_authored_by": true,
						"generated_with": false
					}
				}
			}`,
			expectedTrailer:  TrailerStyleAssistedBy,
			expectedGenerate: false,
		},
		{
			name: "default when neither setting present",
			configJSON: `{
				"options": {
					"attribution": {
						"generated_with": true
					}
				}
			}`,
			expectedTrailer:  TrailerStyleNone,
			expectedGenerate: true,
		},
		{
			name: "default when attribution is null",
			configJSON: `{
				"options": {}
			}`,
			expectedTrailer:  TrailerStyleNone,
			expectedGenerate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := loadFromBytes([][]byte{[]byte(tt.configJSON)})
			require.NoError(t, err)

			cfg.setDefaults(t.TempDir(), "")

			require.Equal(t, tt.expectedTrailer, cfg.Options.Attribution.TrailerStyle)
			require.Equal(t, tt.expectedGenerate, cfg.Options.Attribution.GeneratedWith)
		})
	}
}

func TestAnswerShortPromptDefault(t *testing.T) {
	t.Parallel()

	cfg, err := loadFromBytes([][]byte{[]byte(`{"options": {}}`)})
	require.NoError(t, err)
	cfg.setDefaults(t.TempDir(), "")

	require.Equal(t, defaultAnswerShortPrompt, cfg.Options.AnswerShortPrompt)
	require.Equal(t, "Jawab singkat, jangan perlu banyak mikir", cfg.Options.AnswerShortPrompt)
}

func TestAnswerShortPromptPreserved(t *testing.T) {
	t.Parallel()

	cfg, err := loadFromBytes([][]byte{[]byte(`{
		"options": {
			"answer_short_prompt": "Answer in one short sentence"
		}
	}`)})
	require.NoError(t, err)
	cfg.setDefaults(t.TempDir(), "")

	require.Equal(t, "Answer in one short sentence", cfg.Options.AnswerShortPrompt)
}
