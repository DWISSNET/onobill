# ONOBILL V1 FINAL CHECKLIST
- Phase 1: Auth, Tenant, Dashboard [DONE]
- Phase 2: Router + Direct API [DONE]
- Phase 3: VPN + WireGuard [DONE]
- Phase 4: Customer + PPPoE [DONE]
- Phase 5: Billing + Isolation [DONE]
- Phase 6: Hotspot + Voucher [DONE]
- Phase 7: Reports [DONE]
- Struktur sesuai PRD bagian 51 [DONE]
- Multi-tenant [DONE]
- Job queue / retry [DONE]
- Audit log [DONE]
- Persistent VPN [DONE]

# V1.1 — Auto-Isolation (DONE 2026-09-20)
- RouterOS API integration (go-routeros/v3) [DONE]
- Auto-isolate overdue customers via hourly worker [DONE]
- Auto-unisolate on payment (hook in PayInvoice) [DONE]
- Manual isolate/unisolate via web form & REST API [DONE]
  - POST /api/v1/customers/{id}/isolate
  - POST /api/v1/customers/{id}/unisolate
- PPPoE secret disabled=yes/no sync ke MikroTik [DONE]
