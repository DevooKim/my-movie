package discordbot

import "github.com/bwmarrin/discordgo"

func discordChoices(choices []Choice) []*discordgo.ApplicationCommandOptionChoice {
	result := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(choices))
	for _, choice := range choices {
		result = append(result, &discordgo.ApplicationCommandOptionChoice{Name: choice.Name, Value: choice.Value})
	}
	return result
}

func optionValues(options []*discordgo.ApplicationCommandInteractionDataOption) map[string]string {
	values := make(map[string]string)
	for _, option := range options {
		if option.Type == discordgo.ApplicationCommandOptionString {
			values[option.Name] = option.StringValue()
		}
	}
	return values
}

func focusedOption(options []*discordgo.ApplicationCommandInteractionDataOption) (string, string) {
	for _, option := range options {
		if option.Focused {
			return option.Name, option.StringValue()
		}
	}
	return "", ""
}
