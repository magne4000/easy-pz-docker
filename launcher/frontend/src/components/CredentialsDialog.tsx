import { InfoIcon } from "lucide-react";
import { type FormEvent, useState } from "react";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

export type Credentials = { username: string; password: string; serverPassword: string };

export function CredentialsDialog({
  open,
  onOpenChange,
  serverName,
  defaultUsername,
  hasSavedServerPassword,
  onSubmit,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  serverName: string;
  defaultUsername: string;
  hasSavedServerPassword: boolean;
  onSubmit: (c: Credentials) => void;
}) {
  const { t } = useTranslation();
  const [username, setUsername] = useState(defaultUsername);
  const [password, setPassword] = useState("");
  const [serverPassword, setServerPassword] = useState("");

  const submit = (e: FormEvent) => {
    e.preventDefault();
    onSubmit({ username: username.trim(), password, serverPassword });
    setPassword("");
    setServerPassword("");
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="space-y-4">
          <DialogHeader>
            <DialogTitle>{t("Log in to {{name}}", { name: serverName })}</DialogTitle>
            <DialogDescription>{t("Your account on this server.")}</DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="cred-user">{t("Username")}</Label>
            <Input
              id="cred-user"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              autoFocus={!defaultUsername}
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="cred-pw">{t("Password")}</Label>
            <Input
              id="cred-pw"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              autoFocus={!!defaultUsername}
              required
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="cred-srv">{t("Server password")}</Label>
            <Input
              id="cred-srv"
              type="password"
              value={serverPassword}
              onChange={(e) => setServerPassword(e.target.value)}
              placeholder={
                hasSavedServerPassword
                  ? t("Leave empty to use the saved one")
                  : t("Only if the server has one")
              }
            />
          </div>
          <Alert>
            <InfoIcon />
            <AlertDescription>
              {t(
                "The launcher does not remember passwords. To skip this step next time, join the server once from the game with “Save password” ticked: the launcher will then use that saved account.",
              )}
            </AlertDescription>
          </Alert>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t("Cancel")}
            </Button>
            <Button type="submit">{t("Play")}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
