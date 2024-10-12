# Golang Twilio Voice + OpenAI Integration

This project integrates Twilio Voice with OpenAI's language models using Golang. It enables AI-powered voice interactions, allowing for intelligent phone call handling, voice-based chatbots, or automated customer service systems.

## Features

- Seamless integration of Twilio Voice API and OpenAI's GPT models
- Real-time speech-to-text and text-to-speech conversion
- Dynamic conversation flow based on AI responses
- Customizable AI personality and knowledge base
- Scalable architecture for handling multiple concurrent calls

## Prerequisites

- Go 1.16 or higher
- Twilio account and API credentials
- OpenAI API key

## Installation

1. Clone the repository:
   ```
   git clone https://github.com/shakibhasan09/twilio-voice-openai.git
   cd twilio-voice-openai
   ```

2. Install dependencies:
   ```
   go mod tidy
   ```

3. Set up environment variables:
   ```
   cp .env.example .env
   ```

## Usage

1. Start the server:
   ```
   go run main.go
   ```

2. Configure your Twilio phone number to point to your server's webhook URL.

3. Make a call to your Twilio number to interact with the AI-powered voice system.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgements

- [Twilio Go Library](https://github.com/twilio/twilio-go)
- [OpenAI Go Client](https://github.com/sashabaranov/go-openai)
- [Gorilla Mux Router](https://github.com/gorilla/mux)

## Contact

If you have any questions or feedback, please open an issue on this repository or contact the maintainer at your@email.com.
