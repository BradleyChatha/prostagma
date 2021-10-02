module app;

import std, jcli, server, client;

int main(string[] args)
{
    return (new CommandLineInterface!app).parseAndExecute(args);
}

@Command("server", "Runs the server")
struct ServerCommand
{
    void onExecute()
    {
        serverMain();
    }
}

@Command("client", "Runs a client")
struct ClientCommand
{
    void onExecute()
    {
        clientMain();
    }
}

@Command("test", "Performs a test trigger")
struct TestCommand
{
    @ArgPositional("file", "The trigger script to test.")
    string file;

    void onExecute()
    {
        import vibe.d;
        setLogLevel(LogLevel.trace);
        auto uuid = clientRegister();
        auto socket = initConnection(uuid);
        scope(exit) socket.close();
        onTrigger(this.file, socket);
    }
}