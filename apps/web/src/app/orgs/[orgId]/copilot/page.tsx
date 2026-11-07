import { BotIcon } from "lucide-react";
import { ComingSoon } from "@/components/coming-soon";

export default function CopilotPage() {
  return (
    <ComingSoon
      title="AI Copilot"
      description="Ask questions about your business in plain language."
      icon={BotIcon}
      features={[
        { title: "Conversations", body: "Threaded chats that remember context, private to your workspace." },
        { title: "Grounded answers", body: "Responses cite the data sources and queries they used." },
        { title: "Save to dashboard", body: "Pin any answer as a widget in one click." },
      ]}
      note="Connect a data source first — the copilot answers from your data."
    />
  );
}
