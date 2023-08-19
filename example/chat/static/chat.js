console.log("evo me");

const ws = new WebSocket("ws://127.0.0.1:3000/ws");

ws.onmessage = (event) => {
  console.log("onmessage", event.data);
  addEntry(event.data);
};

ws.onerror = (event) => {
  console.error(event.data);
};

ws.onclose = (event) => {
  console.log("close", event);
};

ws.onopen = () => {
  ws.send("Here's some text that the server is urgently awaiting!");
  for (var i = 0; i < 5; i++) {
    ws.send("msg " + i);
  }
}

function send() {
  const input = document.getElementById("data").value;
  ws.send(input);
  return false;
}

function addEntry(s) {
  const p = document.createElement("pre");
  p.appendChild(document.createTextNode(s));
  document.getElementById("chats").appendChild(p);

  document.getElementById("send").scrollIntoView();
}

window.chat = {
  send: send,
};
