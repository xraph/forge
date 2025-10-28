# Forge License Decision Tree

Quick decision guide: **Do I need a commercial license?**

```
                        ┌─────────────────────────────┐
                        │   Using Forge?              │
                        └──────────┬──────────────────┘
                                   │
                     ┌─────────────┴──────────────┐
                     │                            │
            ┌────────▼─────────┐       ┌─────────▼─────────┐
            │   Using ONLY     │       │   Using AI        │
            │   Forge Core +   │       │   Extension?      │
            │   Non-AI Exts    │       └─────────┬─────────┘
            └────────┬─────────┘                 │
                     │                            │
            ┌────────▼─────────┐       ┌─────────▼─────────┐
            │   ✅ FREE!       │       │   What purpose?   │
            │   MIT License    │       └─────────┬─────────┘
            │                  │                  │
            │   Use for:       │     ┌────────────┼────────────┐
            │   • Personal     │     │            │            │
            │   • Commercial   │  ┌──▼───┐   ┌───▼───┐   ┌───▼────┐
            │   • Any purpose  │  │ Per- │   │ Edu/  │   │ Comme- │
            └──────────────────┘  │ sonal│   │ Resea-│   │ rcial  │
                                  │      │   │ rch   │   │ Prod   │
                                  └──┬───┘   └───┬───┘   └───┬────┘
                                     │           │           │
                                  ┌──▼───────────▼────┐   ┌──▼──────┐
                                  │   ✅ FREE!         │   │ ❌ Need │
                                  │                    │   │ License │
                                  │   • Personal proj  │   └─────────┘
                                  │   • Learning       │
                                  │   • Research       │
                                  │   • Evaluation     │
                                  └────────────────────┘
```

## Quick Reference

### ✅ NO LICENSE NEEDED

**Using Forge Core + Any Non-AI Extensions:**
- Personal projects → **Free**
- Commercial products → **Free**
- Startups → **Free**
- Enterprises → **Free**
- Open source → **Free**

**Using AI Extension for:**
- Personal projects → **Free**
- Learning/education → **Free**
- Academic research → **Free**
- Evaluation (90 days) → **Free**

### ❌ COMMERCIAL LICENSE NEEDED

**Using AI Extension for:**
- Production SaaS → **License Required**
- Commercial product → **License Required**
- Internal business tools → **License Required**
- Revenue-generating apps → **License Required**
- After 90-day evaluation → **License Required**

## Examples by Role

### 👨‍💻 Individual Developer
**Building a side project**
- Using any Forge components → ✅ **Free**

**Building portfolio/learning**
- Using AI Extension → ✅ **Free**

**Building SaaS product**
- Without AI Extension → ✅ **Free**
- With AI Extension → ❌ **License Required**

### 🏢 Startup
**Internal tools (pre-revenue)**
- Evaluation period → ✅ **Free** (90 days)
- After evaluation → ❌ **License Required**

**Customer-facing product**
- Without AI Extension → ✅ **Free**
- With AI Extension → ❌ **License Required**

### 🏛️ Enterprise
**Evaluation/POC**
- 90 days → ✅ **Free**

**Production deployment**
- Without AI Extension → ✅ **Free**
- With AI Extension → ❌ **License Required**
- Volume licensing available

### 🎓 Student/Researcher
**Any use case**
- Everything → ✅ **Free**
- Including AI Extension

### 🏗️ Agency/Consultant
**Building for clients**
- Development → ✅ **Free**
- Client's production (non-AI) → ✅ **Free**
- Client's production (with AI) → ❌ **Client needs license**

## By Extension

| Extension | License | Commercial Use |
|-----------|---------|----------------|
| Forge Core | MIT | ✅ Free |
| auth | MIT | ✅ Free |
| cache | MIT | ✅ Free |
| consensus | MIT | ✅ Free |
| dashboard | MIT | ✅ Free |
| database | MIT | ✅ Free |
| events | MIT | ✅ Free |
| graphql | MIT | ✅ Free |
| grpc | MIT | ✅ Free |
| hls | MIT | ✅ Free |
| kafka | MIT | ✅ Free |
| mcp | MIT | ✅ Free |
| mqtt | MIT | ✅ Free |
| orpc | MIT | ✅ Free |
| queue | MIT | ✅ Free |
| search | MIT | ✅ Free |
| storage | MIT | ✅ Free |
| streaming | MIT | ✅ Free |
| webrtc | MIT | ✅ Free |
| **ai** | **Commercial** | **❌ License Required** |

## Contact

Need help deciding?

📧 **Email**: licensing@xraph.com  
🌐 **Web**: https://github.com/xraph/forge  
💬 **Issues**: https://github.com/xraph/forge/issues

---

## Full Documentation

For detailed information, see:

- [LICENSE](LICENSE) - MIT License for Forge Core
- [LICENSING.md](LICENSING.md) - Complete licensing guide
- [LICENSE_FAQ.md](LICENSE_FAQ.md) - Common questions
- [extensions/ai/LICENSE](extensions/ai/LICENSE) - AI Extension license
- [extensions/ai/LICENSE_NOTICE.md](extensions/ai/LICENSE_NOTICE.md) - AI summary

---

**Rule of thumb**: If AI Extension is helping you make money, you need a license. Otherwise, it's probably free! 🎯

