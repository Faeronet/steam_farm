mod ipc;
mod monitor;
mod proc_kill;
mod process;
mod sandbox;
mod steam;

use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "sfarm-sandbox", about = "Lightweight game sandbox with process isolation")]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Launch a new sandbox instance (stays in foreground, emits JSON events to stdout)
    Launch {
        #[arg(long, help = "JSON configuration for the sandbox")]
        config: String,
    },
    /// Stop a running sandbox by ID
    Stop {
        #[arg(long)]
        id: u64,
    },
    /// List all running sandboxes
    Status,
}

#[tokio::main]
async fn main() {
    let cli = Cli::parse();

    match cli.command {
        Commands::Launch { config } => {
            let cfg: ipc::LaunchConfig = match serde_json::from_str(&config) {
                Ok(c) => c,
                Err(e) => {
                    ipc::emit(&ipc::IpcEvent::Error {
                        message: format!("Invalid config JSON: {}", e),
                    });
                    std::process::exit(1);
                }
            };

            if let Err(e) = sandbox::run(cfg).await {
                ipc::emit(&ipc::IpcEvent::Error {
                    message: e.to_string(),
                });
                std::process::exit(1);
            }
        }
        Commands::Stop { id } => {
            sandbox::stop(id);
        }
        Commands::Status => {
            sandbox::status();
        }
    }
}
