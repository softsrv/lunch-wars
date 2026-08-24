## Overview

You and your friends need help deciding where to go for lunch. Instead of endless debate and discussion, let MuchBot do the picking. MunchBot is a Discord application that joins your server and help you decide on lunch via ranked choice voting.

## how it works

- MunchBot announces in a channel that it's time to decide on a restaurant. This can be initiated by a user or scheduled for a particular time.
- Users must use `/join` to join the vote.
- Each participant will send in up to 3 restaurants via slash command, separated by commas `/nominate situ, cracklemi, marketpho`
- MunchBot compiles one numbered list of all the restaurants suggested. duplicates will be combined (case insensitive)
- Munchbot prints the list in the channel and asks for vetos.
- each user can `/veto` up to 1 restaurant from the list. that restaurant will be eliminated
- there must be at least 3 choices left for the final vote
- Munchbot then prints the revised list in the channel and asks for ranked votes.
- Each user can choose up to 3 places from the list, in order of preference `/vote marketpho, situ, cracklemi`
- MunchBot will use the ranked choice voting process to select a winner. One final message will be sent that shows each iteration of the process and the final winner.

## tech stack

- this will have no web frontend
- this application will have a user model to track who is voting, but users cannot log in, there is no authorization/authentication other than what is required to interact with Discord itself.
- postgresdb should be used for creating "elections" and any other models required for the functionality
- election history will be kept in the db but is not accessible in any way except for what is exposed via slash commands.
- the backend API will be written in Golang, using `https://github.com/bwmarrin/discordgo` as the main interface with Discord.
