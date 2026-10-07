import { type FormEvent, useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
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
import { type Config, errorMessage, Launcher } from "@/lib/api";

export function EditServerDialog({
  open,
  onOpenChange,
  server,
  onSaved,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  server: Config.Server;
  onSaved: () => void;
}) {
  const { t } = useTranslation();
  const [name, setName] = useState(server.name);
  const [host, setHost] = useState(server.host);
  const [port, setPort] = useState(String(server.port || 16261));

  useEffect(() => {
    if (open) {
      setName(server.name);
      setHost(server.host);
      setPort(String(server.port || 16261));
    }
  }, [open, server]);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    try {
      await Launcher.UpdateServer(server.id, name, host, Number(port), server.account ?? "");
      onSaved();
    } catch (err) {
      toast.error(errorMessage(err));
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form onSubmit={submit} className="space-y-4">
          <DialogHeader>
            <DialogTitle>{t("Edit server")}</DialogTitle>
            {server.pageUrl && (
              <DialogDescription>
                {t("The address is updated automatically when the server publishes it.")}
              </DialogDescription>
            )}
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="edit-name">{t("Name")}</Label>
            <Input id="edit-name" value={name} onChange={(e) => setName(e.target.value)} required />
          </div>
          <div className="grid grid-cols-[1fr_7rem] gap-3">
            <div className="space-y-2">
              <Label htmlFor="edit-host">{t("Address")}</Label>
              <Input
                id="edit-host"
                value={host}
                onChange={(e) => setHost(e.target.value)}
                placeholder="pz.example.com"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="edit-port">{t("Port")}</Label>
              <Input
                id="edit-port"
                type="number"
                min={1}
                max={65535}
                value={port}
                onChange={(e) => setPort(e.target.value)}
              />
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t("Cancel")}
            </Button>
            <Button type="submit">{t("Save")}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
